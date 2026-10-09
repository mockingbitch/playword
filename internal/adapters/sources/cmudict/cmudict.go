// Package cmudict reads the CMU Pronouncing Dictionary (ARPAbet), the
// English pronunciation source (spec §1.6, ADR-003 D2).
package cmudict

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"playword/internal/adapters/sources/fetch"
	"playword/internal/ports"
)

// DefaultURL is the maintained CMUdict on GitHub.
const DefaultURL = "https://raw.githubusercontent.com/cmusphinx/cmudict/master/cmudict.dict"

// License of the dictionary data.
const License = "BSD-2-Clause — CMU Pronouncing Dictionary, Carnegie Mellon University"

// Source is CMUdict.
type Source struct {
	url    string
	client *http.Client
}

// New returns the source; empty url and nil client mean the defaults.
func New(url string, client *http.Client) *Source {
	if url == "" {
		url = DefaultURL
	}
	return &Source{url: url, client: client}
}

// Name is "cmudict".
func (s *Source) Name() string { return "cmudict" }

// URL is where Fetch downloads from.
func (s *Source) URL() string { return s.url }

// Fetch downloads the dictionary.
func (s *Source) Fetch(ctx context.Context, since time.Time) (io.ReadCloser, ports.Meta, error) {
	return fetch.Get(ctx, s.client, s.url, since)
}

// Parse emits each word's first (primary) pronunciation. Alternatives
// ("read(2)") and comments ("# …") are skipped.
func Parse(ctx context.Context, r io.Reader, emit func(word string, phones []string) error) error {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, _, _ := strings.Cut(sc.Text(), "#")
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.HasSuffix(fields[0], ")") {
			continue
		}
		if err := emit(strings.ToLower(fields[0]), fields[1:]); err != nil {
			return err
		}
	}
	return sc.Err()
}
