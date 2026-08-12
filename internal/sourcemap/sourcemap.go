// Package sourcemap discovers and verifies JavaScript source maps.
package sourcemap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jshound/jshound/internal/input"
)

var (
	directivePattern = regexp.MustCompile(`(?i)sourceMappingURL\s*=\s*["']?([^\s"'<>*]+)`)
	mapLinkPattern   = regexp.MustCompile(`["']([^"'<>\s]+\.map(?:\?[^"'<>\s]*)?(?:#[^"'<>\s]*)?)["']`)
	mapAttrPattern   = regexp.MustCompile(`(?i)(?:src|href)\s*=\s*([^\s"'=<>` + "`" + `]+\.map(?:\?[^\s"'<>` + "`" + `]*)?(?:#[^\s"'<>` + "`" + `]*)?)`)
)

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
}

type Checker struct {
	fetcher *input.Fetcher
}

func NewChecker(opts CheckerOptions) *Checker {
	return &Checker{fetcher: input.NewFetcher(input.Options{
		MaxBytes: opts.MaxBytes, Timeout: opts.Timeout, UserAgent: opts.UserAgent,
	})}
}

// Discover finds sourceMappingURL directives and explicit quoted .map links.
// Relative references are resolved when sourceURL is an absolute HTTP(S) URL.
func Discover(data []byte, sourceURL string) []Reference {
	values := make([]string, 0)
	for _, match := range directivePattern.FindAllSubmatch(data, -1) {
		values = append(values, strings.TrimSpace(string(match[1])))
	}
	for _, match := range mapLinkPattern.FindAllSubmatch(data, -1) {
		values = append(values, strings.TrimSpace(string(match[1])))
	}
	for _, match := range mapAttrPattern.FindAllSubmatch(data, -1) {
		values = append(values, strings.TrimSpace(string(match[1])))
	}

	seen := make(map[string]struct{}, len(values))
	refs := make([]Reference, 0, len(values))
	for _, raw := range values {
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
		return reference
	}
	return base.ResolveReference(ref).String()
}

// Check retrieves a source map and verifies its required JSON structure.
func (c *Checker) Check(ctx context.Context, ref Reference) Result {
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
			return result
		}
		data = decoded
		result.Live = true
	} else {
		source, err := c.fetcher.Load(ctx, target)
		result.StatusCode = source.StatusCode
		result.Live = (source.StatusCode >= 200 && source.StatusCode < 300) || (source.StatusCode == 0 && err == nil)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		data = source.Data
	}

	metadata, err := Validate(data)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Valid = true
	result.Metadata = &metadata
	return result
}

// Validate checks the required version, sources, and mappings members.
func Validate(data []byte) (Metadata, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Metadata{}, fmt.Errorf("invalid source-map JSON: %w", err)
	}
	for _, name := range []string{"version", "sources", "mappings"} {
		if _, ok := fields[name]; !ok {
			return Metadata{}, fmt.Errorf("invalid source map: missing %s", name)
		}
	}

	var version int
	if err := json.Unmarshal(fields["version"], &version); err != nil || version != 3 {
		return Metadata{}, fmt.Errorf("invalid source map: version must be 3")
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
		var content []any
		if err := json.Unmarshal(raw, &content); err != nil {
			return Metadata{}, fmt.Errorf("invalid source map: sourcesContent must be an array or null")
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
