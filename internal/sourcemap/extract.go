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
}

type Extraction struct {
	Map     string
	Written []string
	Skipped []string
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
// requested output directory. Missing embedded content is fetched relative to
// the map when it resolves to a local file or HTTP(S) URL.
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
	result := Extraction{Map: mapName}
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
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return fmt.Errorf("create source directory: %w", err)
		}
		file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return fmt.Errorf("create source %q: %w", sourceName, err)
		}
		if _, err := file.Write(content); err != nil {
			file.Close()
			return fmt.Errorf("write source %q: %w", sourceName, err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("close source %q: %w", sourceName, err)
		}
		result.Written = append(result.Written, destination)
	}
	return nil
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
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		value = strings.Trim(parsed.Host+"/"+parsed.Path, "/")
	}
	value = strings.ReplaceAll(value, "\\", "/")
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
