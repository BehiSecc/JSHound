// Package input provides bounded loading of local and remote analysis inputs.
package input

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	DefaultMaxBytes  int64 = 32 << 20
	DefaultTimeout         = 15 * time.Second
	defaultUserAgent       = "JSHound/1.0"
)

var ErrTooLarge = errors.New("input exceeds maximum size")

// Options controls resource use while loading inputs.
type Options struct {
	MaxBytes  int64
	Timeout   time.Duration
	UserAgent string
	Client    *http.Client
}

// Source is a fully loaded, bounded analysis input.
type Source struct {
	Name        string
	URL         *url.URL
	Data        []byte
	ContentType string
	StatusCode  int
}

// Fetcher loads local files and HTTP resources using the same size policy.
type Fetcher struct {
	maxBytes  int64
	timeout   time.Duration
	userAgent string
	client    *http.Client
}

func NewFetcher(opts Options) *Fetcher {
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	userAgent := opts.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	return &Fetcher{maxBytes: maxBytes, timeout: timeout, userAgent: userAgent, client: client}
}

func (f *Fetcher) MaxBytes() int64 { return f.maxBytes }

// Fetch retrieves an HTTP(S) URL. Non-2xx responses are returned as errors.
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (Source, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Source{}, fmt.Errorf("parse URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Source{}, fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}
	requestCtx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Source{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "text/html, application/javascript, text/javascript, application/json, */*;q=0.1")

	resp, err := f.client.Do(req)
	if err != nil {
		return Source{}, fmt.Errorf("fetch %s: %w", u.Redacted(), err)
	}
	defer resp.Body.Close()
	result := Source{
		Name:        u.String(),
		URL:         u,
		ContentType: resp.Header.Get("Content-Type"),
		StatusCode:  resp.StatusCode,
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("fetch %s: HTTP %s", u.Redacted(), resp.Status)
	}
	if resp.ContentLength > f.maxBytes {
		return result, fmt.Errorf("fetch %s: %w (%d byte limit)", u.Redacted(), ErrTooLarge, f.maxBytes)
	}

	data, err := f.readAll(resp.Body)
	if err != nil {
		return result, fmt.Errorf("fetch %s: %w", u.Redacted(), err)
	}
	result.Data = data
	return result, nil
}

// ReadFile loads a local file with the configured size bound.
func (f *Fetcher) ReadFile(path string) (Source, error) {
	file, err := os.Open(path)
	if err != nil {
		return Source{}, err
	}
	defer file.Close()
	if info, statErr := file.Stat(); statErr == nil && info.Size() > f.maxBytes {
		return Source{}, fmt.Errorf("%s: %w (%d byte limit)", path, ErrTooLarge, f.maxBytes)
	}
	data, err := f.readAll(file)
	if err != nil {
		return Source{}, fmt.Errorf("read %s: %w", path, err)
	}
	return Source{Name: path, Data: data}, nil
}

// Read consumes a stream with the configured size bound.
func (f *Fetcher) Read(name string, reader io.Reader) (Source, error) {
	data, err := f.readAll(reader)
	if err != nil {
		return Source{}, fmt.Errorf("read %s: %w", name, err)
	}
	return Source{Name: name, Data: data}, nil
}

// FromBytes accepts an already loaded input while still enforcing the bound.
func (f *Fetcher) FromBytes(name string, data []byte) (Source, error) {
	if int64(len(data)) > f.maxBytes {
		return Source{}, fmt.Errorf("%s: %w (%d byte limit)", name, ErrTooLarge, f.maxBytes)
	}
	return Source{Name: name, Data: data}, nil
}

// Load treats HTTP(S) values as URLs and everything else as a local path.
func (f *Fetcher) Load(ctx context.Context, value string) (Source, error) {
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return f.Fetch(ctx, value)
	}
	return f.ReadFile(value)
}

func (f *Fetcher) readAll(reader io.Reader) ([]byte, error) {
	limited := io.LimitReader(reader, f.maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > f.maxBytes {
		return nil, fmt.Errorf("%w (%d byte limit)", ErrTooLarge, f.maxBytes)
	}
	return data, nil
}
