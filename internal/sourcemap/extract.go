package sourcemap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type ExtractOptions struct {
	MaxBytes int64
	Output   string
	// MaxFiles bounds how many files one map can create; 0 means
	// MaxExtractedFiles.
	MaxFiles int
}

// MaxExtractedFiles is the default bound on files created from one map.
const MaxExtractedFiles = 50000

// Extraction reports what Extract wrote. Skipped lists sources that were not
// written: no embedded content, a duplicate path, a name the file system
// rejects, or past the file limit (Truncated). Failed counts the rejected
// names, and FirstError says why the first of them failed.
type Extraction struct {
	Map        string
	Written    []string
	Skipped    []string
	Failed     int
	FirstError error
	Truncated  bool
	maxFiles   int
}

type mapDocument struct {
	Version        int          `json:"version"`
	SourceRoot     string       `json:"sourceRoot"`
	Sources        []string     `json:"sources"`
	SourcesContent []*string    `json:"sourcesContent"`
	Mappings       *string      `json:"mappings"`
	Sections       []mapSection `json:"sections"`
}
type mapSection struct {
	Map json.RawMessage `json:"map"`
}

// Extract validates a source map and reconstructs its source tree below the
// requested output directory from the content embedded in the map. Sources
// without embedded content are skipped; their paths are never fetched.
func Extract(_ context.Context, mapName string, data []byte, opts ExtractOptions) (Extraction, error) {
	if opts.Output == "" {
		return Extraction{}, fmt.Errorf("source-map output directory is required")
	}
	if _, err := Validate(data); err != nil {
		return Extraction{}, err
	}
	root, err := filepath.Abs(opts.Output)
	if err != nil {
		return Extraction{}, fmt.Errorf("resolve output directory: %w", err)
	}
	if err := ensureEmptyDirectory(root); err != nil {
		return Extraction{}, err
	}
	result := Extraction{Map: mapName, maxFiles: opts.MaxFiles}
	if result.maxFiles <= 0 {
		result.maxFiles = MaxExtractedFiles
	}
	used := make(map[string]struct{})
	if err := extractDocument(root, data, &result, used); err != nil {
		return Extraction{}, err
	}
	if len(result.Written) == 0 {
		return result, fmt.Errorf("source map contains no extractable source content")
	}
	return result, nil
}

func extractDocument(root string, data []byte, result *Extraction, used map[string]struct{}) error {
	var doc mapDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("decode source map: %w", err)
	}
	for _, section := range doc.Sections {
		if err := extractDocument(root, section.Map, result, used); err != nil {
			return err
		}
	}
	for i, sourceName := range doc.Sources {
		content, ok := embeddedContent(doc.SourcesContent, i)
		if !ok {
			result.Skipped = append(result.Skipped, sourceName)
			continue
		}
		if len(result.Written) >= result.maxFiles {
			result.Skipped = append(result.Skipped, sourceName)
			result.Truncated = true
			continue
		}
		relative := safeSourcePath(doc.SourceRoot, sourceName, i)
		destination := filepath.Join(root, relative)
		if !withinRoot(root, destination) {
			return fmt.Errorf("unsafe source path %q", sourceName)
		}
		if _, exists := used[destination]; exists {
			result.Skipped = append(result.Skipped, sourceName)
			continue
		}
		used[destination] = struct{}{}
		// A name the file system rejects, or one that clashes with a directory
		// of another source, skips that source; the rest are still written.
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			result.skip(sourceName, err)
			continue
		}
		file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			result.skip(sourceName, err)
			continue
		}
		if _, err := file.Write(content); err != nil {
			file.Close()
			os.Remove(destination)
			return fmt.Errorf("write source %q: %w", sourceName, err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close source %q: %w", sourceName, err)
		}
		result.Written = append(result.Written, destination)
	}
	return nil
}

func (e *Extraction) skip(sourceName string, err error) {
	e.Skipped = append(e.Skipped, sourceName)
	if e.Failed == 0 {
		e.FirstError = err
	}
	e.Failed++
}

func embeddedContent(values []*string, index int) ([]byte, bool) {
	if index >= len(values) || values[index] == nil {
		return nil, false
	}
	return []byte(*values[index]), true
}

func safeSourcePath(sourceRoot, sourceName string, index int) string {
	value := sourceName
	if sourceRoot != "" {
		value = strings.TrimSuffix(sourceRoot, "/") + "/" + strings.TrimPrefix(sourceName, "/")
	}
	// Windows paths (C:\src\app.ts) keep their names: separators become
	// slashes before the drive letter is read as a URL scheme and dropped.
	value = strings.ReplaceAll(value, "\\", "/")
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		value = strings.Trim(parsed.Host+"/"+parsed.Path, "/")
	}
	for _, prefix := range []string{"webpack://", "webpack:/", "file://", "file:/"} {
		value = strings.TrimPrefix(value, prefix)
	}
	clean := path.Clean("/" + strings.TrimLeft(value, "/"))
	parts := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	safe := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			continue
		}
		part = strings.ReplaceAll(part, ":", "_")
		safe = append(safe, part)
	}
	if len(safe) == 0 {
		return fmt.Sprintf("source-%d.js", index)
	}
	return filepath.Join(safe...)
}

func ensureEmptyDirectory(root string) error {
	if info, err := os.Stat(root); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("source-map output is not a directory: %s", root)
		}
		entries, readErr := os.ReadDir(root)
		if readErr != nil {
			return fmt.Errorf("read output directory: %w", readErr)
		}
		if len(entries) != 0 {
			return fmt.Errorf("source-map output directory must be empty: %s", root)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect output directory: %w", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	return nil
}

func withinRoot(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
