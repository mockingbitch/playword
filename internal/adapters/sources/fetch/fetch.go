// Package fetch downloads source dumps over HTTP.
package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"playword/internal/ports"
)

// DefaultClient allows large dumps to download.
var DefaultClient = &http.Client{Timeout: 5 * time.Minute}

// Get downloads url; with a non-zero since it asks the server to skip an
// unchanged file and reports NotModified.
func Get(ctx context.Context, client *http.Client, url string, since time.Time) (io.ReadCloser, ports.Meta, error) {
	if client == nil {
		client = DefaultClient
	}
	meta := ports.Meta{URL: url}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, meta, err
	}
	if !since.IsZero() {
		req.Header.Set("If-Modified-Since", since.UTC().Format(http.TimeFormat))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, meta, err
	}
	meta.ETag = resp.Header.Get("ETag")
	meta.LastModified, _ = http.ParseTime(resp.Header.Get("Last-Modified"))
	switch resp.StatusCode {
	case http.StatusOK:
		return resp.Body, meta, nil
	case http.StatusNotModified:
		_ = resp.Body.Close()
		meta.NotModified = true
		return io.NopCloser(strings.NewReader("")), meta, nil
	}
	_ = resp.Body.Close()
	return nil, meta, fmt.Errorf("GET %s: %s", url, resp.Status)
}
