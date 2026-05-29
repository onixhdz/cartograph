package search

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"regexp/syntax"
	"slices"
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

// BuildRegexIndex builds the regex search index from in-memory file contents
// and atomically publishes it under dir.
func BuildRegexIndex(dir string, files []RegexBuildFile) (RegexBuildStats, error) {
	b := newRegexIndexBuilder()
	for _, file := range files {
		path := cleanRegexPath(file.Path)
		if path == "" {
			continue
		}
		if err := b.add(path, bytes.NewReader(file.Data)); err != nil {
			return RegexBuildStats{}, fmt.Errorf("regex index: index %s: %w", path, err)
		}
	}
	return publishRegexIndex(dir, b)
}

// publishRegexIndex serializes the builder and atomically replaces dir/index
// via a single closed file: write to a temp file, fsync, close, validate, then
// rename. Building in memory and publishing one already-closed file avoids the
// leaked handles, mmap'd temp files, and directory renames that fail on Windows
// (which opens files without FILE_SHARE_DELETE).
func publishRegexIndex(dir string, b *regexIndexBuilder) (RegexBuildStats, error) {
	stats := RegexBuildStats{Files: len(b.paths), Bytes: b.bytes}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return RegexBuildStats{}, fmt.Errorf("regex index: create dir: %w", err)
	}
	data, err := b.encode()
	if err != nil {
		return RegexBuildStats{}, err
	}
	tmpPath := filepath.Join(dir, RegexIndexFile+".tmp")
	if err := writeFileSync(tmpPath, data); err != nil {
		return RegexBuildStats{}, err
	}
	if err := validateRegexIndex(tmpPath, stats.Files); err != nil {
		_ = os.Remove(tmpPath)
		return RegexBuildStats{}, err
	}
	if err := os.Rename(tmpPath, filepath.Join(dir, RegexIndexFile)); err != nil {
		_ = os.Remove(tmpPath)
		return RegexBuildStats{}, fmt.Errorf("regex index: publish: %w", err)
	}
	return stats, nil
}

// writeFileSync writes data to path, flushing to stable storage and closing the
// file before returning so the file is never left open for a later rename.
func writeFileSync(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("regex index: create temp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return fmt.Errorf("regex index: write temp: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return fmt.Errorf("regex index: sync temp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("regex index: close temp: %w", err)
	}
	return nil
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

// BuildRegexIndexFromOpener builds the regex search index by reading each path
// through openFile and atomically publishes it under dir. Each reader is closed
// before the next path is opened.
func BuildRegexIndexFromOpener(dir string, paths []string, openFile func(string) (io.ReadCloser, error)) (RegexBuildStats, error) {
	b := newRegexIndexBuilder()
	for _, path := range paths {
		path = cleanRegexPath(path)
		if path == "" {
			continue
		}
		r, err := openFile(path)
		if err != nil {
			return RegexBuildStats{}, fmt.Errorf("regex index: open %s: %w", path, err)
		}
		addErr := b.add(path, r)
		closeErr := r.Close()
		if addErr != nil {
			return RegexBuildStats{}, fmt.Errorf("regex index: index %s: %w", path, addErr)
		}
		if closeErr != nil {
			return RegexBuildStats{}, fmt.Errorf("regex index: close %s: %w", path, closeErr)
		}
	}
	return publishRegexIndex(dir, b)
}

// Text-detection limits mirror github.com/google/codesearch so the index we
// build is byte-for-byte compatible with the on-disk format the reader parses.
const (
	regexMaxFileLen      = 1 << 30
	regexMaxLineLen      = 2000
	regexMaxTextTrigrams = 20000
	regexSentinelTrigram = uint32(1<<24 - 1)
)

// regexIndexBuilder accumulates the codesearch index in memory. paths holds
// every candidate path (indexed or not) so unindexed files fall back to a
// linear scan; names and postings cover only files that pass text detection.
type regexIndexBuilder struct {
	paths    []string
	names    []string
	postings map[uint32][]uint32
	bytes    int64
}

func newRegexIndexBuilder() *regexIndexBuilder {
	return &regexIndexBuilder{postings: make(map[uint32][]uint32)}
}

// add records path as a candidate and, if its content is valid indexable text,
// assigns it a file ID and records its trigrams. Files are processed in order,
// so each posting list is naturally appended in ascending file-ID order.
func (b *regexIndexBuilder) add(path string, r io.Reader) error {
	b.paths = append(b.paths, path)
	trigrams, n, indexed := extractRegexTrigrams(r)
	b.bytes += n
	if !indexed {
		return nil
	}
	fileID, err := regexOffset(len(b.names))
	if err != nil {
		return err
	}
	b.names = append(b.names, path)
	for trigram := range trigrams {
		b.postings[trigram] = append(b.postings[trigram], fileID)
	}
	return nil
}

// extractRegexTrigrams streams r and collects its distinct trigrams, applying
// the same text-detection rules as codesearch. It returns indexed=false when
// the content is not indexable text, so the caller keeps the path as an
// unindexed fallback candidate.
//
// A read error (or a non-progressing zero-length read) marks the file
// unindexed rather than failing the build, matching codesearch: an unreadable
// entry such as a symlink-to-directory must not abort indexing the whole repo.
func extractRegexTrigrams(r io.Reader) (map[uint32]struct{}, int64, bool) {
	trigrams := make(map[uint32]struct{})
	buf := make([]byte, 16384)
	var (
		tv      uint32
		n       int64
		linelen int
		i       int
		filled  int
	)
	for {
		tv = (tv << 8) & (1<<24 - 1)
		if i >= filled {
			m, err := r.Read(buf)
			if m == 0 {
				if errors.Is(err, io.EOF) {
					break
				}
				return nil, n, false
			}
			filled = m
			i = 0
		}
		c := buf[i]
		i++
		tv |= uint32(c)
		if n++; n >= 3 {
			trigrams[tv] = struct{}{}
		}
		if !validRegexUTF8((tv>>8)&0xFF, tv&0xFF) {
			return nil, n, false
		}
		if n > regexMaxFileLen {
			return nil, n, false
		}
		if linelen++; linelen > regexMaxLineLen {
			return nil, n, false
		}
		if c == '\n' {
			linelen = 0
		}
		if len(trigrams) > regexMaxTextTrigrams {
			return nil, n, false
		}
	}
	return trigrams, n, true
}

// validRegexUTF8 reports whether the byte pair can appear in a valid sequence
// of UTF-8-encoded code points. Copied from codesearch to match its text
// detection exactly.
func validRegexUTF8(c1, c2 uint32) bool {
	switch {
	case c1 < 0x80:
		return c2 < 0x80 || 0xc0 <= c2 && c2 < 0xf8
	case c1 < 0xc0:
		return c2 < 0xf8
	case c1 < 0xf8:
		return 0x80 <= c2 && c2 < 0xc0
	}
	return false
}

// encode serializes the builder into the codesearch on-disk index format that
// parseRegexDiskIndex reads. Sections are laid out as: magic, path list, name
// data, posting lists, name index, posting index, trailer.
//
// The codesearch on-disk format stores all section offsets as uint32, so an
// index larger than 4GB cannot be represented; encode returns an error in that
// case rather than writing a corrupt index.
func (b *regexIndexBuilder) encode() ([]byte, error) {
	const magic = "csearch index 1\n"
	const trailerMagic = "\ncsearch trailr\n"

	buf := new(bytes.Buffer)
	buf.WriteString(magic)

	pathData, err := regexOffset(buf.Len())
	if err != nil {
		return nil, err
	}
	for _, p := range b.paths {
		buf.WriteString(p)
		buf.WriteByte(0)
	}
	buf.WriteByte(0)

	nameData, err := regexOffset(buf.Len())
	if err != nil {
		return nil, err
	}
	nameOffsets := make([]uint32, 0, len(b.names)+1)
	for _, name := range b.names {
		off, err := regexOffset(buf.Len())
		if err != nil {
			return nil, err
		}
		nameOffsets = append(nameOffsets, off-nameData)
		buf.WriteString(name)
		buf.WriteByte(0)
	}
	// Trailing empty name terminates the name table, matching codesearch.
	off, err := regexOffset(buf.Len())
	if err != nil {
		return nil, err
	}
	nameOffsets = append(nameOffsets, off-nameData)
	buf.WriteByte(0)

	postData, err := regexOffset(buf.Len())
	if err != nil {
		return nil, err
	}
	trigrams := make([]uint32, 0, len(b.postings))
	for trigram := range b.postings {
		trigrams = append(trigrams, trigram)
	}
	slices.Sort(trigrams)

	type postIndexEntry struct {
		trigram uint32
		nfile   uint32
		offset  uint32
	}
	entries := make([]postIndexEntry, 0, len(trigrams)+1)
	appendTrigram := func(t uint32) {
		var scratch [4]byte
		binary.BigEndian.PutUint32(scratch[:], t)
		buf.Write(scratch[1:])
	}
	for _, trigram := range trigrams {
		posting := b.postings[trigram]
		offset, err := regexOffset(buf.Len())
		if err != nil {
			return nil, err
		}
		appendTrigram(trigram)
		fileID := ^uint32(0)
		for _, id := range posting {
			writeRegexUvarint(buf, id-fileID)
			fileID = id
		}
		writeRegexUvarint(buf, 0)
		nfile, err := regexOffset(len(posting))
		if err != nil {
			return nil, err
		}
		entries = append(entries, postIndexEntry{trigram, nfile, offset - postData})
	}
	// Sentinel trigram marks the end of the posting lists.
	sentinelOffset, err := regexOffset(buf.Len())
	if err != nil {
		return nil, err
	}
	appendTrigram(regexSentinelTrigram)
	writeRegexUvarint(buf, 0)
	entries = append(entries, postIndexEntry{regexSentinelTrigram, 0, sentinelOffset - postData})

	nameIndex, err := regexOffset(buf.Len())
	if err != nil {
		return nil, err
	}
	for _, nameOff := range nameOffsets {
		writeRegexUint32(buf, nameOff)
	}

	postIndex, err := regexOffset(buf.Len())
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		appendTrigram(e.trigram)
		writeRegexUint32(buf, e.nfile)
		writeRegexUint32(buf, e.offset)
	}

	for _, v := range []uint32{pathData, nameData, postData, nameIndex, postIndex} {
		writeRegexUint32(buf, v)
	}
	buf.WriteString(trailerMagic)
	return buf.Bytes(), nil
}

// regexOffset converts a buffer length to a uint32 index offset, rejecting
// indexes that exceed the codesearch format's 4GB addressing limit.
func regexOffset(n int) (uint32, error) {
	if n < 0 || n > int(^uint32(0)) {
		return 0, fmt.Errorf("regex index: size %d exceeds 4GB limit", n)
	}
	return uint32(n), nil
}

func writeRegexUint32(buf *bytes.Buffer, x uint32) {
	var scratch [4]byte
	binary.BigEndian.PutUint32(scratch[:], x)
	buf.Write(scratch[:])
}

func writeRegexUvarint(buf *bytes.Buffer, x uint32) {
	var tmp [binary.MaxVarintLen32]byte
	n := binary.PutUvarint(tmp[:], uint64(x))
	buf.Write(tmp[:n])
}
