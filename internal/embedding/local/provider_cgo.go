// Package local provides embedding via native CGO-linked inference.
package local

/*
#cgo LDFLAGS: -L${SRCDIR}/zig-out/lib -linference -lc++
#cgo darwin LDFLAGS: -F${SRCDIR}/zig-out/lib -framework CoreFoundation -framework Security
#cgo CFLAGS: -I${SRCDIR}/zig-out/include

#include "inference.h"
#include <stdlib.h>
*/
import "C"
import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"unsafe"

	"github.com/realxen/cartograph/internal/sysutil"
)

// nativeMemPerWorker is the approximate memory each native worker uses.
// Conservative estimate: context KV cache + I/O buffers. Models with large
// context windows or higher dimensions will use more, but we cap context
// at 8192 in the C shim to keep this bounded.
const nativeMemPerWorker = 200 << 20

// defaultWorkerCount returns worker count based on system resources.
func defaultWorkerCount() int {
	if env := os.Getenv("CARTOGRAPH_EMBEDDING_WORKERS"); env != "" {
		if n, err := strconv.Atoi(env); err == nil && n >= 1 {
			return n
		}
	}

	cpus := runtime.NumCPU()
	n := cpus / 2 // each worker gets ≥2 batch threads → total ≈ cpus

	if avail := sysutil.AvailableMemory(); avail > 0 {
		maxByMem := int(avail * 3 / 4 / nativeMemPerWorker)
		if maxByMem < n {
			n = maxByMem
		}
	}

	if n < 1 {
		n = 1
	}
	return n
}

// worker wraps a single C-side cg_model instance for one goroutine.
type worker struct {
	mu    sync.Mutex // guards model during inference
	model *C.cg_model
	dims  int
	maxSq int
}

// tokenize uses llama.cpp's built-in tokenizer to convert text to IDs.
func (w *worker) tokenize(text string) ([]int32, error) {
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))

	maxTokens := w.maxSq
	ids := make([]int32, maxTokens)

	n := int(C.cg_tokenize(w.model, cText, (*C.int32_t)(unsafe.Pointer(&ids[0])), C.int(maxTokens), C.int(1)))
	if n < 0 {
		// Buffer overflow — allocate bigger buffer and retry.
		maxTokens = -n + 16
		ids = make([]int32, maxTokens)
		n = int(C.cg_tokenize(w.model, cText, (*C.int32_t)(unsafe.Pointer(&ids[0])), C.int(maxTokens), C.int(1)))
		if n < 0 {
			return nil, fmt.Errorf("llamacpp: tokenization failed (need %d tokens)", -n)
		}
	}

	return ids[:n], nil
}

// embedOne runs inference for pre-tokenised IDs on this worker's model.
func (w *worker) embedOne(ids []int32, dims int) ([]float32, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	seqLen := len(ids)
	if seqLen > w.maxSq {
		seqLen = w.maxSq
	}

	inputPtr := C.cg_model_input_ids(w.model)
	maskPtr := C.cg_model_attn_mask(w.model)

	cInput := unsafe.Slice((*int32)(unsafe.Pointer(inputPtr)), seqLen)
	cMask := unsafe.Slice((*int32)(unsafe.Pointer(maskPtr)), seqLen)
	copy(cInput, ids[:seqLen])
	// Set attention mask for real tokens.
	for i := range seqLen {
		cMask[i] = 1
	}

	rc := C.cg_embed_tokens(w.model, C.int(seqLen))
	if rc != 0 {
		return nil, fmt.Errorf("llamacpp: cg_embed_tokens returned %d", int(rc))
	}

	outPtr := C.cg_model_output(w.model)
	cOut := unsafe.Slice((*float32)(unsafe.Pointer(outPtr)), dims)
	vec := make([]float32, dims)
	copy(vec, cOut)

	return vec, nil
}

// embedText tokenizes and embeds a single text string.
func (w *worker) embedText(text string, dims int) ([]float32, error) {
	ids, err := w.tokenize(text)
	if err != nil {
		return nil, err
	}
	return w.embedOne(ids, dims)
}

// TokenCount returns the number of tokens for the given text using the
// model's built-in tokenizer. Thread-safe (uses worker mutex).
func (w *worker) TokenCount(text string) int {
	w.mu.Lock()
	defer w.mu.Unlock()

	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))

	maxTokens := w.maxSq
	ids := make([]int32, maxTokens)

	n := int(C.cg_tokenize(w.model, cText, (*C.int32_t)(unsafe.Pointer(&ids[0])), C.int(maxTokens), C.int(0)))
	if n < 0 {
		return -n // overflow means at least this many tokens
	}
	return n
}

// Provider implements the embedding.Provider interface using native
// CGO-linked llama.cpp. Each worker has its own cg_model instance with
// per-model I/O buffers for thread safety.
type Provider struct {
	workers  []*worker
	dims     int
	maxSeq   int
	modelTmp string
}

// New creates a native CGO provider with default settings.
func New(modelBytes []byte) (*Provider, error) {
	return NewWithWorkers(modelBytes, defaultWorkerCount())
}

// NewWithWorkers creates a provider with the specified worker count.
func NewWithWorkers(modelBytes []byte, n int) (*Provider, error) {
	if len(modelBytes) == 0 {
		return nil, fmt.Errorf("llamacpp: no model data provided")
	}
	if n < 1 {
		n = 1
	}

	// C API requires a file path, not a byte buffer.
	tmpFile, err := os.CreateTemp("", "cartograph-model-*.gguf")
	if err != nil {
		return nil, fmt.Errorf("llamacpp: create temp model: %w", err)
	}
	modelPath := tmpFile.Name()

	if _, err := tmpFile.Write(modelBytes); err != nil {
		tmpFile.Close()
		os.Remove(modelPath)
		return nil, fmt.Errorf("llamacpp: write temp model: %w", err)
	}
	tmpFile.Close()

	cPath := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cPath))

	workers := make([]*worker, 0, n)
	var dims, maxSeq int

	threadsPerWorker := runtime.NumCPU() / n
	if threadsPerWorker < 2 {
		threadsPerWorker = 2
	}

	cleanup := func() {
		for _, w := range workers {
			C.cg_model_free(w.model)
		}
		os.Remove(modelPath)
	}

	for i := 0; i < n; i++ {
		m := C.cg_model_init(cPath, C.int(threadsPerWorker))
		if m == nil {
			cleanup()
			return nil, fmt.Errorf("llamacpp: cg_model_init failed for worker %d", i)
		}

		d := int(C.cg_model_dims(m))
		ms := int(C.cg_model_max_seq(m))

		if i == 0 {
			dims = d
			maxSeq = ms
		}

		workers = append(workers, &worker{
			model: m,
			dims:  d,
			maxSq: ms,
		})
	}

	return &Provider{
		workers:  workers,
		dims:     dims,
		maxSeq:   maxSeq,
		modelTmp: modelPath,
	}, nil
}

// Embed generates embeddings for the given texts using parallel native workers.
func (p *Provider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	nWorkers := len(p.workers)
	results := make([][]float32, len(texts))

	// Fast path: single text or single worker
	if len(texts) == 1 || nWorkers == 1 {
		for i, text := range texts {
			vec, err := p.workers[0].embedText(text, p.dims)
			if err != nil {
				return nil, err
			}
			results[i] = vec
		}
		return results, nil
	}

	// Parallel path: partition texts across workers
	chunkSize := (len(texts) + nWorkers - 1) / nWorkers
	errs := make([]error, nWorkers)
	var wg sync.WaitGroup

	for wi := 0; wi < nWorkers; wi++ {
		lo := wi * chunkSize
		if lo >= len(texts) {
			break
		}
		hi := lo + chunkSize
		if hi > len(texts) {
			hi = len(texts)
		}

		wg.Add(1)
		go func(workerIdx, lo, hi int) {
			defer wg.Done()
			w := p.workers[workerIdx]
			for i := lo; i < hi; i++ {
				vec, err := w.embedText(texts[i], p.dims)
				if err != nil {
					errs[workerIdx] = err
					return
				}
				results[i] = vec
			}
		}(wi, lo, hi)
	}

	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return results, err
		}
	}
	return results, nil
}

// TokenCount returns the token count for text using the model's tokenizer.
// Uses the first worker. Safe for concurrent use.
func (p *Provider) TokenCount(text string) int {
	return p.workers[0].TokenCount(text)
}

// Workers returns the number of parallel native workers.
func (p *Provider) Workers() int { return len(p.workers) }

func (p *Provider) Dimensions() int { return p.dims }
func (p *Provider) Name() string    { return fmt.Sprintf("llamacpp(%d workers)", len(p.workers)) }

func (p *Provider) Close() error {
	for _, w := range p.workers {
		if w.model != nil {
			C.cg_model_free(w.model)
			w.model = nil
		}
	}
	if p.modelTmp != "" {
		os.Remove(p.modelTmp)
	}
	return nil
}
