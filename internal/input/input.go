// Package input provides bounded loading of local and remote analysis inputs.
package input

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
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

var (
	ErrTooLarge = errors.New("input exceeds maximum size")
	// ErrNonPublic is returned for a host on a loopback, private, link-local,
	// or other non-public address when the fetcher only reaches public hosts.
	ErrNonPublic = errors.New("refusing to fetch a non-public address")
)

// Options controls resource use while loading inputs.
type Options struct {
	MaxBytes  int64
	Timeout   time.Duration
	UserAgent string
	Client    *http.Client
	// PublicOnly refuses hosts, and redirects to hosts, that resolve to a
	// non-public address. It is for URLs taken from analyzed content.
	PublicOnly bool
}

// Source is a fully loaded, bounded analysis input.
type Source struct {
	Name        string
	URL         *url.URL
	Data        []byte
	ContentType string
	StatusCode  int
	SourceMap   string
	// Local is true for a file read from a local path, not a URL or stdin.
	Local bool
}

// Fetcher loads local files and HTTP resources using the same size policy.
type Fetcher struct {
	maxBytes   int64
	timeout    time.Duration
	userAgent  string
	client     *http.Client
	publicOnly bool
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
		if opts.PublicOnly {
			client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("stopped after 10 redirects")
				}
				return checkPublic(req.Context(), req.URL.Hostname())
			}
		}
	}
	userAgent := opts.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	return &Fetcher{maxBytes: maxBytes, timeout: timeout, userAgent: userAgent, client: client, publicOnly: opts.PublicOnly}
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
	if f.publicOnly {
		if err := checkPublic(requestCtx, u.Hostname()); err != nil {
			return Source{}, fmt.Errorf("fetch %s: %w", u.Redacted(), err)
		}
	}
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
	// Name and URL never carry credentials from the URL, so they stay out of
	// findings. URL is where the content was served from, after redirects,
	// and is what relative references resolve against.
	name, final := *u, *resp.Request.URL
	name.User, final.User = nil, nil
	result := Source{
		Name:        name.String(),
		URL:         &final,
		ContentType: resp.Header.Get("Content-Type"),
		StatusCode:  resp.StatusCode,
	}
	result.SourceMap = strings.TrimSpace(resp.Header.Get("SourceMap"))
	if result.SourceMap == "" {
		result.SourceMap = strings.TrimSpace(resp.Header.Get("X-SourceMap"))
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
	return Source{Name: path, Data: data, Local: true}, nil
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
	if IsURL(value) {
		return f.Fetch(ctx, value)
	}
	return f.ReadFile(value)
}

// IsPublicHost reports whether host is a name or address on the public
// internet: not localhost, and not resolving to a loopback, private,
// link-local, shared (CGNAT), or otherwise non-public address. A name that
// does not resolve is treated as public, since a proxy may still reach it.
func IsPublicHost(ctx context.Context, host string) bool {
	return checkPublic(ctx, host) == nil
}

func checkPublic(ctx context.Context, host string) error {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("%w: %s", ErrNonPublic, host)
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if !publicAddr(addr) {
			return fmt.Errorf("%w: %s", ErrNonPublic, host)
		}
		return nil
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil
	}
	for _, addr := range addrs {
		if !publicAddr(addr) {
			return fmt.Errorf("%w: %s resolves to %s", ErrNonPublic, host, addr)
		}
	}
	return nil
}

var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
}

func publicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// IsURL reports whether value is an HTTP(S) URL; the scheme is case-insensitive.
func IsURL(value string) bool {
	lower := strings.ToLower(value[:min(len(value), len("https://"))])
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
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
