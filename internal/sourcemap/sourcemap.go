// Package sourcemap discovers and verifies JavaScript source maps.
package sourcemap

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/BehiSecc/JSHound/internal/input"
)

var (
	directivePattern = regexp.MustCompile(`(?i)sourceMappingURL\s*=\s*["']?([^\s"'<>*]+)`)
	mapLinkPattern   = regexp.MustCompile(`["']([^"'<>\s]+\.map(?:\?[^"'<>\s]*)?(?:#[^"'<>\s]*)?)["']`)
	mapAttrPattern   = regexp.MustCompile(`(?i)(?:src|href)\s*=\s*([^\s"'=<>` + "`" + `]+\.map(?:\?[^\s"'<>` + "`" + `]*)?(?:#[^\s"'<>` + "`" + `]*)?)`)
)

const MaxReferences = 64

// Reference describes a discovered source-map reference and its resolved URL.
type Reference struct {
	Raw string `json:"raw"`
	URL string `json:"url"`
}

// Metadata is the useful, bounded summary of a valid source map.
type Metadata struct {
	Version     int      `json:"version"`
	Sources     []string `json:"sources"`
	SourceCount int      `json:"source_count"`
	HasContent  bool     `json:"has_sources_content"`
}

// Result separates reachability from source-map validity.
type Result struct {
	Reference  Reference `json:"reference"`
	Live       bool      `json:"live"`
	Valid      bool      `json:"valid"`
	StatusCode int       `json:"status_code,omitempty"`
	Metadata   *Metadata `json:"metadata,omitempty"`
	Error      string    `json:"error,omitempty"`
}

type CheckerOptions struct {
	MaxBytes  int64
	Timeout   time.Duration
	UserAgent string
	// PublicOnly refuses map URLs on non-public addresses; see input.Options.
	PublicOnly bool
}

type Checker struct {
	fetcher *input.Fetcher
}

func NewChecker(opts CheckerOptions) *Checker {
	return &Checker{fetcher: input.NewFetcher(input.Options{
		MaxBytes: opts.MaxBytes, Timeout: opts.Timeout, UserAgent: opts.UserAgent, PublicOnly: opts.PublicOnly,
	})}
}

// Load retrieves a map reference and returns its validated bytes.
func (c *Checker) Load(ctx context.Context, ref Reference) ([]byte, Result) {
	result := Result{Reference: ref}
	target := ref.URL
	if target == "" {
		target = ref.Raw
	}
	var data []byte
	if strings.HasPrefix(target, "data:") {
		decoded, err := decodeDataURL(target, c.fetcher.MaxBytes())
		if err != nil {
			result.Error = err.Error()
			return nil, result
		}
		data, result.Live = decoded, true
	} else {
		// A map path comes from analyzed content: read only a regular file,
		// never a device or pipe that could block or never end.
		if !input.IsURL(target) {
			if info, err := os.Stat(target); err == nil && !info.Mode().IsRegular() {
				result.Error = fmt.Sprintf("%s: not a regular file", target)
				return nil, result
			}
		}
		source, err := c.fetcher.Load(ctx, target)
		result.StatusCode = source.StatusCode
		result.Live = (source.StatusCode >= 200 && source.StatusCode < 300) || (source.StatusCode == 0 && err == nil)
		if err != nil {
			result.Error = err.Error()
			return nil, result
		}
		data = source.Data
	}
	metadata, err := Validate(data)
	if err != nil {
		result.Error = err.Error()
		return nil, result
	}
	result.Valid, result.Metadata = true, &metadata
	return data, result
}

// Discover finds sourceMappingURL directives and explicit quoted .map links.
// Relative references are resolved when sourceURL is an absolute HTTP(S) URL.
func Discover(data []byte, sourceURL string) []Reference {
	values := make([]string, 0)
	if bytes.Contains(data, []byte("sourceMappingURL")) {
		for _, match := range directivePattern.FindAllSubmatch(data, -1) {
			values = append(values, strings.TrimSpace(string(match[1])))
		}
	}
	// Explicit .map links/attributes are relevant in HTML, but scanning
	// minified JavaScript for them creates expensive member-access matches.
	prefix := data
	if len(prefix) > 512 {
		prefix = prefix[:512]
	}
	trimmed := strings.TrimSpace(string(prefix))
	lowerPrefix := strings.ToLower(trimmed)
	if strings.HasPrefix(lowerPrefix, "<!doctype html") || strings.HasPrefix(lowerPrefix, "<html") || strings.HasPrefix(lowerPrefix, "<script") {
		for _, match := range mapLinkPattern.FindAllSubmatch(data, -1) {
			values = append(values, strings.TrimSpace(string(match[1])))
		}
		for _, match := range mapAttrPattern.FindAllSubmatch(data, -1) {
			values = append(values, strings.TrimSpace(string(match[1])))
		}
	}

	seen := make(map[string]struct{}, len(values))
	refs := make([]Reference, 0, len(values))
	for _, raw := range values {
		if len(refs) >= MaxReferences {
			break
		}
		raw = strings.TrimSuffix(strings.TrimSuffix(raw, "*/"), "-->")
		if raw == "" {
			continue
		}
		resolved := Resolve(sourceURL, raw)
		key := resolved
		if key == "" {
			key = raw
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		refs = append(refs, Reference{Raw: raw, URL: resolved})
	}
	return refs
}

// Resolve resolves a reference against the URL of the containing HTML/JS page.
func Resolve(sourceURL, reference string) string {
	ref, err := url.Parse(reference)
	if err != nil {
		return ""
	}
	if ref.IsAbs() || strings.HasPrefix(reference, "//") || strings.HasPrefix(reference, "data:") {
		if strings.HasPrefix(reference, "//") {
			if base, baseErr := url.Parse(sourceURL); baseErr == nil && base.Scheme != "" {
				ref.Scheme = base.Scheme
			}
		}
		return ref.String()
	}
	base, err := url.Parse(sourceURL)
	if err != nil || !base.IsAbs() || (base.Scheme != "http" && base.Scheme != "https") {
		if sourceURL != "" && !strings.Contains(sourceURL, "://") {
			localReference := reference
			if parsedReference, parseErr := url.Parse(reference); parseErr == nil {
				localReference = parsedReference.Path
			}
			return filepath.Clean(filepath.Join(filepath.Dir(sourceURL), localReference))
		}
		return reference
	}
	return base.ResolveReference(ref).String()
}

// Check retrieves a source map and verifies its required JSON structure.
func (c *Checker) Check(ctx context.Context, ref Reference) Result {
	_, result := c.Load(ctx, ref)
	return result
}

// Validate checks regular and indexed source-map v3 structures.
func Validate(data []byte) (Metadata, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Metadata{}, fmt.Errorf("invalid source-map JSON: %w", err)
	}
	if _, ok := fields["version"]; !ok {
		return Metadata{}, fmt.Errorf("invalid source map: missing version")
	}

	var version int
	if err := json.Unmarshal(fields["version"], &version); err != nil || version != 3 {
		return Metadata{}, fmt.Errorf("invalid source map: version must be 3")
	}
	if sectionsRaw, indexed := fields["sections"]; indexed {
		var sections []struct {
			Map json.RawMessage `json:"map"`
		}
		if err := json.Unmarshal(sectionsRaw, &sections); err != nil || len(sections) == 0 {
			return Metadata{}, fmt.Errorf("invalid indexed source map: sections must be a non-empty array")
		}
		metadata := Metadata{Version: version}
		for _, section := range sections {
			if len(section.Map) == 0 {
				return Metadata{}, fmt.Errorf("invalid indexed source map: section missing map")
			}
			child, err := Validate(section.Map)
			if err != nil {
				return Metadata{}, err
			}
			metadata.Sources = append(metadata.Sources, child.Sources...)
			metadata.SourceCount += child.SourceCount
			metadata.HasContent = metadata.HasContent || child.HasContent
		}
		return metadata, nil
	}
	for _, name := range []string{"sources", "mappings"} {
		if _, ok := fields[name]; !ok {
			return Metadata{}, fmt.Errorf("invalid source map: missing %s", name)
		}
	}
	var sources []string
	if err := json.Unmarshal(fields["sources"], &sources); err != nil {
		return Metadata{}, fmt.Errorf("invalid source map: sources must be a string array")
	}
	var mappings string
	if err := json.Unmarshal(fields["mappings"], &mappings); err != nil {
		return Metadata{}, fmt.Errorf("invalid source map: mappings must be a string")
	}
	_ = mappings // An empty mappings string is valid for an empty generated file.

	metadata := Metadata{Version: version, Sources: sources, SourceCount: len(sources)}
	if raw, ok := fields["sourcesContent"]; ok && string(raw) != "null" {
		var content []*string
		if err := json.Unmarshal(raw, &content); err != nil {
			return Metadata{}, fmt.Errorf("invalid source map: sourcesContent must be a string/null array or null")
		}
		if len(content) != len(sources) {
			return Metadata{}, fmt.Errorf("invalid source map: sourcesContent count differs from sources")
		}
		metadata.HasContent = len(content) > 0
	}
	return metadata, nil
}

func decodeDataURL(value string, maxBytes int64) ([]byte, error) {
	comma := strings.IndexByte(value, ',')
	if comma < 0 {
		return nil, fmt.Errorf("invalid source-map data URL")
	}
	header, payload := value[:comma], value[comma+1:]
	var data []byte
	var err error
	if strings.HasSuffix(strings.ToLower(header), ";base64") {
		data, err = base64.StdEncoding.DecodeString(payload)
	} else {
		var decoded string
		decoded, err = url.PathUnescape(payload)
		data = []byte(decoded)
	}
	if err != nil {
		return nil, fmt.Errorf("decode source-map data URL: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("source map: %w (%s byte limit)", input.ErrTooLarge, strconv.FormatInt(maxBytes, 10))
	}
	return data, nil
}
