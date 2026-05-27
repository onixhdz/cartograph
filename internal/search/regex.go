package search

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"regexp/syntax"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	codesearch "github.com/google/codesearch/index"
)

const (
	RegexIndexVersion   = "codesearch-v1"
	RegexIndexFile      = "index"
	RegexStatusIndexed  = "indexed"
	RegexStatusDegraded = "degraded"

	DefaultRegexSearchLimit   = 20
	MaxRegexSearchLimit       = 200
	DefaultRegexContextLines  = 1
	MaxRegexContextLines      = 5
	MaxRegexFallbackFiles     = 5000
	MaxRegexFallbackBytes     = int64(256 * 1024 * 1024)
	MaxRegexReturnedLineBytes = 2000
)

var ErrRegexIndexMissing = errors.New("regex index missing")

type RegexBuildFile struct {
	Path string
	Data []byte
}

type RegexBuildStats struct {
	Files int
	Bytes int64
}

type countingReader struct {
	r     io.Reader
	bytes int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.bytes += int64(n)
	return n, err //nolint:wrapcheck // Implements io.Reader; callers need the original read error.
}

type RegexIndex struct {
	path      string
	index     *regexDiskIndex
	files     []string
	unindexed []string
}

type regexDiskIndex struct {
	data      []byte
	pathData  int
	nameData  int
	postData  int
	nameIndex int
	postIndex int
	trailer   int
	numName   int
	numPost   int
	paths     []string
}

type RegexSearchOptions struct {
	Pattern      string
	FixedStrings bool
	IgnoreCase   bool
	Limit        int
	ContextLines int
	FilesGlob    string
	ExcludeTests bool
	AllFiles     []string
	ReadFile     func(string) ([]byte, error)
	IsTestFile   func(string) bool
}

type RegexSearchResult struct {
	Status    string
	Duration  time.Duration
	Matches   []RegexMatch
	FileCount int
	Truncated bool
	Degraded  bool
}

type RegexMatch struct {
	FilePath string
	Line     int
	Column   int
	LineText string
	Before   []string
	After    []string
}

func BuildRegexIndex(dir string, files []RegexBuildFile) (RegexBuildStats, error) {
	return buildRegexIndexWithWriter(dir, func(w *codesearch.IndexWriter) (RegexBuildStats, error) {
		stats := RegexBuildStats{}
		paths := make([]string, 0, len(files))
		for _, file := range files {
			path := cleanRegexPath(file.Path)
			if path == "" {
				continue
			}
			w.Add(path, bytes.NewReader(file.Data))
			paths = append(paths, path)
			stats.Files++
			stats.Bytes += int64(len(file.Data))
		}
		w.AddPaths(paths)
		return stats, nil
	})
}

func buildRegexIndexWithWriter(dir string, add func(*codesearch.IndexWriter) (RegexBuildStats, error)) (RegexBuildStats, error) {
	if err := os.MkdirAll(filepath.Dir(dir), 0o750); err != nil {
		return RegexBuildStats{}, fmt.Errorf("regex index: create parent: %w", err)
	}
	tmpDir := dir + ".tmp"
	if err := os.RemoveAll(tmpDir); err != nil {
		return RegexBuildStats{}, fmt.Errorf("regex index: remove temp: %w", err)
	}
	if err := os.MkdirAll(tmpDir, 0o750); err != nil {
		return RegexBuildStats{}, fmt.Errorf("regex index: create temp: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	idxPath := filepath.Join(tmpDir, RegexIndexFile)
	w := codesearch.Create(idxPath)
	stats, err := add(w)
	if err != nil {
		return RegexBuildStats{}, err
	}
	withDiscardedStdLog(w.Flush)

	if err := validateRegexIndex(idxPath, stats.Files); err != nil {
		return RegexBuildStats{}, err
	}
	oldDir := dir + ".old"
	if err := os.RemoveAll(oldDir); err != nil {
		return RegexBuildStats{}, fmt.Errorf("regex index: remove old backup: %w", err)
	}
	if err := os.Rename(dir, oldDir); err != nil && !os.IsNotExist(err) {
		return RegexBuildStats{}, fmt.Errorf("regex index: move old: %w", err)
	}
	if err := os.Rename(tmpDir, dir); err != nil {
		_ = os.Rename(oldDir, dir)
		return RegexBuildStats{}, fmt.Errorf("regex index: publish: %w", err)
	}
	if err := os.RemoveAll(oldDir); err != nil {
		return RegexBuildStats{}, fmt.Errorf("regex index: remove old backup: %w", err)
	}
	return stats, nil
}

func withDiscardedStdLog(fn func()) {
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(io.Discard)
	defer func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	}()
	fn()
}

func OpenRegexIndex(dir string) (*RegexIndex, error) {
	idxPath := filepath.Join(dir, RegexIndexFile)
	if _, err := os.Stat(idxPath); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrRegexIndexMissing
		}
		return nil, fmt.Errorf("regex index: stat: %w", err)
	}
	ix, err := openRegexDiskIndex(idxPath, -1)
	if err != nil {
		return nil, err
	}
	files := ix.Paths()
	return &RegexIndex{path: idxPath, index: ix, files: files, unindexed: unindexedRegexPaths(ix, files)}, nil
}

func (ix *RegexIndex) Search(opts RegexSearchOptions) (RegexSearchResult, error) {
	started := time.Now()
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultRegexSearchLimit
	}
	limit = min(limit, MaxRegexSearchLimit)
	contextLines := opts.ContextLines
	contextLines = max(contextLines, 0)
	contextLines = min(contextLines, MaxRegexContextLines)
	if opts.ReadFile == nil {
		return RegexSearchResult{}, errors.New("regex search: missing content reader")
	}

	matcher, query, err := newRegexMatcher(opts.Pattern, opts.FixedStrings, opts.IgnoreCase)
	if err != nil {
		return RegexSearchResult{}, err
	}
	degraded := query.Op == codesearch.QAll

	var candidates []string
	if degraded {
		candidates = append(candidates, opts.AllFiles...)
		if len(candidates) == 0 {
			candidates = append(candidates, ix.files...)
		}
	} else {
		postings := ix.index.PostingQuery(query)
		candidates = make([]string, 0, len(postings))
		for _, fileID := range postings {
			candidates = append(candidates, ix.index.Name(fileID))
		}
	}

	matches, fileCount, truncated := verifyRegexCandidates(candidates, matcher, opts, limit, contextLines, degraded)
	if !degraded && len(ix.unindexed) > 0 && len(matches) < limit {
		fallbackMatches, fallbackFileCount, fallbackTruncated := verifyRegexCandidates(ix.unindexed, matcher, opts, limit-len(matches), contextLines, true)
		matches = append(matches, fallbackMatches...)
		fileCount += fallbackFileCount
		truncated = truncated || fallbackTruncated || len(matches) >= limit
		degraded = true
	}
	status := RegexStatusIndexed
	if degraded {
		status = RegexStatusDegraded
	}
	return RegexSearchResult{
		Status:    status,
		Duration:  time.Since(started),
		Matches:   matches,
		FileCount: fileCount,
		Truncated: truncated,
		Degraded:  degraded,
	}, nil
}

func unindexedRegexPaths(ix *regexDiskIndex, files []string) []string {
	indexed := make(map[string]bool)
	for _, fileID := range ix.PostingQuery(&codesearch.Query{Op: codesearch.QAll}) {
		indexed[ix.Name(fileID)] = true
	}
	unindexed := make([]string, 0)
	for _, file := range files {
		file = cleanRegexPath(file)
		if file != "" && !indexed[file] {
			unindexed = append(unindexed, file)
		}
	}
	return unindexed
}

func validateRegexIndex(path string, wantFiles int) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("regex index: validate stat: %w", err)
	}
	_, err := openRegexDiskIndex(path, wantFiles)
	return err
}

func openRegexDiskIndex(path string, wantFiles int) (*regexDiskIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("regex index: validate read: %w", err)
	}
	return parseRegexDiskIndex(data, wantFiles)
}

func parseRegexDiskIndex(data []byte, wantFiles int) (*regexDiskIndex, error) {
	const trailerMagic = "\ncsearch trailr\n"
	const magic = "csearch index 1\n"
	if len(data) < len(magic)+5*4+len(trailerMagic) {
		return nil, errors.New("regex index invalid: too small")
	}
	if string(data[:len(magic)]) != magic {
		return nil, errors.New("regex index invalid: bad header")
	}
	if string(data[len(data)-len(trailerMagic):]) != trailerMagic {
		return nil, errors.New("regex index invalid: bad trailer")
	}

	trailer := len(data) - len(trailerMagic) - 5*4
	pathData := readCodesearchUint32(data, trailer)
	nameData := readCodesearchUint32(data, trailer+4)
	postData := readCodesearchUint32(data, trailer+8)
	nameIndex := readCodesearchUint32(data, trailer+12)
	postIndex := readCodesearchUint32(data, trailer+16)
	if pathData < 0 || nameData < pathData || postData < nameData || nameIndex < postData || postIndex < nameIndex || postIndex > trailer {
		return nil, fmt.Errorf("regex index invalid: offsets path=%d name=%d post=%d nameIndex=%d postIndex=%d trailer=%d", pathData, nameData, postData, nameIndex, postIndex, trailer)
	}
	if (trailer-postIndex)%codesearchPostEntrySize != 0 || (postIndex-nameIndex)%4 != 0 {
		return nil, fmt.Errorf("regex index invalid: section sizes nameIndex=%d postIndex=%d trailer=%d", nameIndex, postIndex, trailer)
	}
	nameCount := (postIndex-nameIndex)/4 - 1
	if nameCount < 0 {
		return nil, errors.New("regex index invalid: negative name count")
	}
	if nameCount > int(^uint32(0)) {
		return nil, errors.New("regex index invalid: too many names")
	}
	paths, err := validateCodesearchPathList(data, pathData, nameData)
	if err != nil {
		return nil, err
	}
	if wantFiles >= 0 && len(paths) != wantFiles {
		return nil, fmt.Errorf("regex index: validate paths: got %d, want %d", len(paths), wantFiles)
	}
	if err := validateCodesearchNames(data, nameData, nameIndex, nameCount); err != nil {
		return nil, err
	}
	if err := validateCodesearchPostings(data, postData, postIndex, trailer, uint32(nameCount)); err != nil {
		return nil, err
	}
	return &regexDiskIndex{
		data:      data,
		pathData:  pathData,
		nameData:  nameData,
		postData:  postData,
		nameIndex: nameIndex,
		postIndex: postIndex,
		trailer:   trailer,
		numName:   nameCount,
		numPost:   (trailer - postIndex) / codesearchPostEntrySize,
		paths:     paths,
	}, nil
}

const codesearchPostEntrySize = 3 + 4 + 4

func readCodesearchUint32(data []byte, off int) int {
	if off < 0 || off+4 > len(data) {
		return -1
	}
	return int(data[off])<<24 | int(data[off+1])<<16 | int(data[off+2])<<8 | int(data[off+3])
}

func validateCodesearchPathList(data []byte, start, end int) ([]string, error) {
	if start < 0 || end < start || end > len(data) {
		return nil, fmt.Errorf("regex index invalid: path list bounds start=%d end=%d size=%d", start, end, len(data))
	}
	paths := make([]string, 0)
	for off := start; ; {
		idx := bytes.IndexByte(data[off:end], 0)
		if idx < 0 {
			return nil, errors.New("regex index invalid: unterminated path list")
		}
		if idx == 0 {
			return paths, nil
		}
		paths = append(paths, string(data[off:off+idx]))
		off += idx + 1
	}
}

func validateCodesearchNames(data []byte, nameData, nameIndex, nameCount int) error {
	for i := 0; i <= nameCount; i++ {
		off := readCodesearchUint32(data, nameIndex+i*4)
		if off < 0 || nameData+off > nameIndex {
			return fmt.Errorf("regex index invalid: name offset %d at %d", off, i)
		}
		if bytes.IndexByte(data[nameData+off:nameIndex], 0) < 0 {
			return fmt.Errorf("regex index invalid: unterminated name at %d", i)
		}
	}
	return nil
}

func validateCodesearchPostings(data []byte, postData, postIndex, trailer int, nameCount uint32) error {
	previousTrigram := -1
	for off := postIndex; off < trailer; off += codesearchPostEntrySize {
		trigram := int(data[off])<<16 | int(data[off+1])<<8 | int(data[off+2])
		if trigram < previousTrigram {
			return fmt.Errorf("regex index invalid: unsorted trigram at %d", off)
		}
		previousTrigram = trigram
		count := readCodesearchUint32(data, off+3)
		postOffset := readCodesearchUint32(data, off+7)
		if count < 0 || postOffset < 0 || postData+postOffset+3 > postIndex {
			return fmt.Errorf("regex index invalid: posting bounds count=%d offset=%d", count, postOffset)
		}
		posting := data[postData+postOffset+3 : postIndex]
		fileID := ^uint32(0)
		for range count {
			delta, n := binary.Uvarint(posting)
			if n <= 0 {
				return fmt.Errorf("regex index invalid: posting varint at %d", off)
			}
			if delta == 0 || delta > uint64(^uint32(0)) {
				return fmt.Errorf("regex index invalid: posting delta at %d", off)
			}
			nextFileID := fileID + uint32(delta)
			if fileID != ^uint32(0) && nextFileID <= fileID {
				return fmt.Errorf("regex index invalid: posting order at %d", off)
			}
			fileID = nextFileID
			if fileID >= nameCount {
				return fmt.Errorf("regex index invalid: posting file id %d >= %d", fileID, nameCount)
			}
			posting = posting[n:]
		}
		if len(posting) == 0 || posting[0] != 0 {
			return fmt.Errorf("regex index invalid: posting terminator at %d", off)
		}
	}
	return nil
}

func (ix *regexDiskIndex) Paths() []string {
	return append([]string(nil), ix.paths...)
}

func (ix *regexDiskIndex) Name(fileID uint32) string {
	if int(fileID) < 0 || int(fileID) >= ix.numName {
		return ""
	}
	off := readCodesearchUint32(ix.data, ix.nameIndex+int(fileID)*4)
	name := ix.nullString(ix.nameData + off)
	return string(name)
}

func (ix *regexDiskIndex) nullString(off int) []byte {
	if off < 0 || off >= len(ix.data) {
		return nil
	}
	end := bytes.IndexByte(ix.data[off:], 0)
	if end < 0 {
		return nil
	}
	return ix.data[off : off+end]
}

func (ix *regexDiskIndex) PostingQuery(q *codesearch.Query) []uint32 {
	return ix.postingQuery(q, nil)
}

func (ix *regexDiskIndex) findList(trigram uint32) (int, int) {
	i := sort.Search(ix.numPost, func(i int) bool {
		off := ix.postIndex + i*codesearchPostEntrySize
		got := uint32(ix.data[off])<<16 | uint32(ix.data[off+1])<<8 | uint32(ix.data[off+2])
		return got >= trigram
	})
	if i >= ix.numPost {
		return 0, 0
	}
	off := ix.postIndex + i*codesearchPostEntrySize
	got := uint32(ix.data[off])<<16 | uint32(ix.data[off+1])<<8 | uint32(ix.data[off+2])
	if got != trigram {
		return 0, 0
	}
	return readCodesearchUint32(ix.data, off+3), readCodesearchUint32(ix.data, off+7)
}

func (ix *regexDiskIndex) postingList(trigram uint32, restrict []uint32) []uint32 {
	count, postOffset := ix.findList(trigram)
	if count == 0 {
		return nil
	}
	data := ix.data[ix.postData+postOffset+3 : ix.postIndex]
	list := make([]uint32, 0, count)
	fileID := ^uint32(0)
	for range count {
		delta, n := binary.Uvarint(data)
		if n <= 0 || delta == 0 || delta > uint64(^uint32(0)) {
			return nil
		}
		data = data[n:]
		nextFileID := fileID + uint32(delta)
		if fileID != ^uint32(0) && nextFileID <= fileID {
			return nil
		}
		fileID = nextFileID
		if restrict != nil && !uint32SortedContains(restrict, fileID) {
			continue
		}
		list = append(list, fileID)
	}
	return list
}

func (ix *regexDiskIndex) postingQuery(q *codesearch.Query, restrict []uint32) []uint32 {
	var list []uint32
	switch q.Op {
	case codesearch.QNone:
		return nil
	case codesearch.QAll:
		if restrict != nil {
			return restrict
		}
		list = make([]uint32, ix.numName)
		for i := range list {
			list[i] = uint32(i)
		}
		return list
	case codesearch.QAnd:
		for _, t := range q.Trigram {
			tri := uint32(t[0])<<16 | uint32(t[1])<<8 | uint32(t[2])
			if list == nil {
				list = ix.postingList(tri, restrict)
			} else {
				list = intersectUint32Sorted(list, ix.postingList(tri, restrict))
			}
			if len(list) == 0 {
				return nil
			}
		}
		for _, sub := range q.Sub {
			if list == nil {
				list = restrict
			}
			list = ix.postingQuery(sub, list)
			if len(list) == 0 {
				return nil
			}
		}
	case codesearch.QOr:
		for _, t := range q.Trigram {
			tri := uint32(t[0])<<16 | uint32(t[1])<<8 | uint32(t[2])
			list = mergeUint32Sorted(list, ix.postingList(tri, restrict))
		}
		for _, sub := range q.Sub {
			list = mergeUint32Sorted(list, ix.postingQuery(sub, restrict))
		}
	}
	return list
}

func uint32SortedContains(values []uint32, target uint32) bool {
	i := sort.Search(len(values), func(i int) bool { return values[i] >= target })
	return i < len(values) && values[i] == target
}

func intersectUint32Sorted(a, b []uint32) []uint32 {
	out := a[:0]
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			i++
		case a[i] > b[j]:
			j++
		default:
			out = append(out, a[i])
			i++
			j++
		}
	}
	return out
}

func mergeUint32Sorted(a, b []uint32) []uint32 {
	out := make([]uint32, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case j == len(b) || (i < len(a) && a[i] < b[j]):
			out = append(out, a[i])
			i++
		case i == len(a) || b[j] < a[i]:
			out = append(out, b[j])
			j++
		default:
			out = append(out, a[i])
			i++
			j++
		}
	}
	return out
}

func cleanRegexPath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	for strings.HasPrefix(path, "./") {
		path = strings.TrimPrefix(path, "./")
	}
	return strings.Trim(path, "/")
}

type regexMatcher struct {
	re *regexp.Regexp
}

func newRegexMatcher(pattern string, fixedStrings, ignoreCase bool) (*regexMatcher, *codesearch.Query, error) {
	if pattern == "" {
		return nil, nil, errors.New("missing pattern")
	}
	if fixedStrings {
		pattern = regexp.QuoteMeta(pattern)
	}
	if ignoreCase {
		pattern = "(?i)" + pattern
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid regex: %w", err)
	}
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid regex: %w", err)
	}
	return &regexMatcher{re: compiled}, codesearch.RegexpQuery(re.Simplify()), nil
}

func verifyRegexCandidates(candidates []string, matcher *regexMatcher, opts RegexSearchOptions, limit, contextLines int, degraded bool) ([]RegexMatch, int, bool) {
	capacity := min(max(limit, 0), 32)
	matches := make([]RegexMatch, 0, capacity)
	if limit <= 0 {
		return matches, 0, false
	}

	seen := make(map[string]bool, len(candidates))
	filesWithMatches := make(map[string]bool)
	var scannedFiles int
	var scannedBytes int64
	truncated := false

	for _, candidate := range candidates {
		filePath := cleanRegexPath(candidate)
		if filePath == "" || seen[filePath] {
			continue
		}
		seen[filePath] = true
		if opts.FilesGlob != "" && !pathGlobMatch(opts.FilesGlob, filePath) {
			continue
		}
		if opts.ExcludeTests && opts.IsTestFile != nil && opts.IsTestFile(filePath) {
			continue
		}
		if degraded {
			if scannedFiles >= MaxRegexFallbackFiles || scannedBytes >= MaxRegexFallbackBytes {
				truncated = true
				break
			}
			scannedFiles++
		}
		data, err := opts.ReadFile(filePath)
		if err != nil || !utf8.Valid(data) {
			continue
		}
		if degraded {
			scannedBytes += int64(len(data))
			if scannedBytes > MaxRegexFallbackBytes {
				truncated = true
				break
			}
		}
		fileMatches := matchesInFile(filePath, data, matcher, contextLines, limit-len(matches))
		if len(fileMatches) == 0 {
			continue
		}
		filesWithMatches[filePath] = true
		matches = append(matches, fileMatches...)
		if len(matches) >= limit {
			truncated = true
			break
		}
	}
	return matches, len(filesWithMatches), truncated
}

func matchesInFile(filePath string, data []byte, matcher *regexMatcher, contextLines, remaining int) []RegexMatch {
	if remaining <= 0 {
		return nil
	}
	lines := splitLines(string(data))
	matches := make([]RegexMatch, 0)
	for i, line := range lines {
		loc := matcher.re.FindStringIndex(line)
		if loc == nil {
			continue
		}
		start := max(0, i-contextLines)
		end := min(len(lines)-1, i+contextLines)
		m := RegexMatch{
			FilePath: filePath,
			Line:     i + 1,
			Column:   utf8.RuneCountInString(line[:loc[0]]) + 1,
			LineText: truncateLine(line),
		}
		for j := start; j < i; j++ {
			m.Before = append(m.Before, truncateLine(lines[j]))
		}
		for j := i + 1; j <= end; j++ {
			m.After = append(m.After, truncateLine(lines[j]))
		}
		matches = append(matches, m)
		if len(matches) >= remaining {
			break
		}
	}
	return matches
}

func splitLines(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return []string{""}
	}
	return strings.Split(content, "\n")
}

func truncateLine(line string) string {
	if len(line) <= MaxRegexReturnedLineBytes {
		return line
	}
	return line[:MaxRegexReturnedLineBytes]
}

func pathGlobMatch(pattern, filePath string) bool {
	pattern = cleanRegexPath(pattern)
	filePath = cleanRegexPath(filePath)
	if pattern == "" {
		return true
	}
	if ok, _ := filepath.Match(pattern, filePath); ok {
		return true
	}
	if !strings.Contains(pattern, "**") {
		return false
	}
	return doubleStarMatch(strings.Split(pattern, "/"), strings.Split(filePath, "/"))
}

func doubleStarMatch(patternParts, pathParts []string) bool {
	if len(patternParts) == 0 {
		return len(pathParts) == 0
	}
	if patternParts[0] == "**" {
		for i := 0; i <= len(pathParts); i++ {
			if doubleStarMatch(patternParts[1:], pathParts[i:]) {
				return true
			}
		}
		return false
	}
	if len(pathParts) == 0 {
		return false
	}
	ok, err := filepath.Match(patternParts[0], pathParts[0])
	if err != nil || !ok {
		return false
	}
	return doubleStarMatch(patternParts[1:], pathParts[1:])
}

func BuildRegexIndexFromOpener(dir string, paths []string, openFile func(string) (io.ReadCloser, error)) (RegexBuildStats, error) {
	return buildRegexIndexWithWriter(dir, func(w *codesearch.IndexWriter) (RegexBuildStats, error) {
		stats := RegexBuildStats{}
		indexedPaths := make([]string, 0, len(paths))
		for _, path := range paths {
			path = cleanRegexPath(path)
			if path == "" {
				continue
			}
			r, err := openFile(path)
			if err != nil {
				return RegexBuildStats{}, fmt.Errorf("regex index: open %s: %w", path, err)
			}
			counter := &countingReader{r: r}
			w.Add(path, counter)
			if err := r.Close(); err != nil {
				return RegexBuildStats{}, fmt.Errorf("regex index: close %s: %w", path, err)
			}
			indexedPaths = append(indexedPaths, path)
			stats.Files++
			stats.Bytes += counter.bytes
		}
		w.AddPaths(indexedPaths)
		return stats, nil
	})
}
