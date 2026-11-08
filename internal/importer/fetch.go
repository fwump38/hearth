// Package importer fetches pages/images, imports the Obsidian vault, and talks to Claude.
package importer

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"

const (
	maxImageBytes = 10 << 20
	maxPageBytes  = 3 << 20
)

// AllowPrivate disables the SSRF guard (tests only).
var AllowPrivate = false

func isPrivate(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast()
}

// safeClient refuses to connect to private/loopback addresses (checked at dial time,
// so redirects and DNS rebinding are covered).
func safeClient(timeout time.Duration) *http.Client {
	d := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			if AllowPrivate {
				return nil
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip != nil && isPrivate(ip) {
				return fmt.Errorf("blocked address %s", host)
			}
			return nil
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{DialContext: d.DialContext, TLSHandshakeTimeout: 10 * time.Second},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
}

func validateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("missing host")
	}
	return u, nil
}

func get(ctx context.Context, rawURL string, limit int64, timeout time.Duration, accept string) ([]byte, string, error) {
	u, err := validateURL(rawURL)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", accept)
	resp, err := safeClient(timeout).Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(b)) > limit {
		return nil, "", errors.New("response too large")
	}
	return b, resp.Header.Get("Content-Type"), nil
}

// FetchPage downloads an HTML page.
func FetchPage(ctx context.Context, rawURL string) (string, error) {
	b, _, err := get(ctx, rawURL, maxPageBytes, 20*time.Second, "text/html,application/xhtml+xml")
	return string(b), err
}

// FetchImage downloads an image and returns its bytes and file extension (".jpg", ...).
func FetchImage(ctx context.Context, rawURL string) ([]byte, string, error) {
	b, ct, err := get(ctx, rawURL, maxImageBytes, 20*time.Second, "image/*")
	if err != nil {
		return nil, "", err
	}
	ext := ImageExt(b, ct)
	if ext == "" {
		return nil, "", errors.New("unsupported image type")
	}
	return b, ext, nil
}

// ImageExt sniffs the image type. Returns "" error-free only for supported types.
func ImageExt(b []byte, contentType string) string {
	ct := http.DetectContentType(b)
	switch ct {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	}
	return ""
}

var (
	reOGImage = regexp.MustCompile(`(?is)<meta[^>]+(?:property|name)\s*=\s*["'](?:og:image(?::secure_url)?|twitter:image)["'][^>]*>`)
	reContent = regexp.MustCompile(`(?is)content\s*=\s*["']([^"']+)["']`)
)

// PageImages returns candidate image URLs (og:image, twitter:image, JSON-LD) from page HTML.
func PageImages(pageURL, page string) []string {
	base, _ := url.Parse(pageURL)
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		s = html.UnescapeString(strings.TrimSpace(s))
		if s == "" {
			return
		}
		if base != nil {
			if u, err := base.Parse(s); err == nil {
				s = u.String()
			}
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, m := range reOGImage.FindAllString(page, -1) {
		if c := reContent.FindStringSubmatch(m); c != nil {
			add(c[1])
		}
	}
	for _, s := range jsonLDImages(page) {
		add(s)
	}
	return out
}

// DownloadFirstImage tries each URL until one yields a supported image.
func DownloadFirstImage(ctx context.Context, urls []string) ([]byte, string, string, error) {
	var last error
	for _, u := range urls {
		b, ext, err := FetchImage(ctx, u)
		if err == nil {
			return b, ext, u, nil
		}
		last = err
	}
	if last == nil {
		last = errors.New("no image candidates")
	}
	return nil, "", "", last
}

func htmlUnescape(s string) string { return html.UnescapeString(s) }
