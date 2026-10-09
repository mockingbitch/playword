// Package wiktionary reads wiktextract JSONL extracts (kaikki.org) of the
// Vietnamese Wiktionary (spec §2.2): definitions in Vietnamese, with
// regional IPA. Senses tagged as slang are emitted as separate IsSlang
// entries so the caller can queue them for review instead of publishing
// them.
package wiktionary

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"playword/internal/adapters/sources/fetch"
	"playword/internal/domain/entry"
	"playword/internal/ports"
)

// DefaultURL is kaikki.org's extract of vi.wiktionary.org.
const DefaultURL = "https://kaikki.org/viwiktionary/raw-wiktextract-data.jsonl.gz"

// Source is the Vietnamese Wiktionary as a ports.Source.
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

// Name is "wiktionary-vi".
func (s *Source) Name() string { return "wiktionary-vi" }

// Kind is a dictionary.
func (s *Source) Kind() ports.SourceKind { return ports.KindDictionary }

// Lang is Vietnamese.
func (s *Source) Lang() entry.Lang { return entry.VI }

// URL is where Fetch downloads from.
func (s *Source) URL() string { return s.url }

// License states the data licence.
func (s *Source) License() string {
	return "CC BY-SA 4.0 — Wiktionary contributors (vi.wiktionary.org), extracted by wiktextract/kaikki.org"
}

// Fetch downloads the extract.
func (s *Source) Fetch(ctx context.Context, since time.Time) (io.ReadCloser, ports.Meta, error) {
	return fetch.Get(ctx, s.client, s.url, since)
}

// Record is one wiktextract line (the fields used here).
type Record struct {
	Word     string  `json:"word"`
	LangCode string  `json:"lang_code"`
	PosTitle string  `json:"pos_title"`
	Senses   []Sense `json:"senses"`
	Sounds   []Sound `json:"sounds"`
}

// Sense is one wiktextract sense.
type Sense struct {
	Glosses  []string `json:"glosses"`
	Tags     []string `json:"tags"`
	Examples []struct {
		Text string `json:"text"`
	} `json:"examples"`
}

// Sound is a pronunciation; Tags name the region ("Hà-Nội", "Huế", "Saigon").
type Sound struct {
	IPA  string   `json:"ipa"`
	Tags []string `json:"tags"`
}

var slangTags = []string{"slang", "Internet"}

// skipTags mark senses that carry no definition of their own.
var skipTags = []string{"no-gloss"}

// Parse emits Vietnamese entries; a word's slang senses become a second,
// IsSlang entry. Gzip input is detected.
func (s *Source) Parse(ctx context.Context, r io.Reader, emit func(entry.Entry) error) error {
	return Each(ctx, r, func(rec Record) error {
		if rec.LangCode != "vi" {
			return nil
		}
		normal := entry.Entry{Headword: rec.Word, Lang: entry.VI, POS: strings.ToLower(rec.PosTitle), SourceRef: "wiktionary-vi:" + rec.Word}
		slang := normal
		slang.IsSlang, slang.Tags = true, []string{"wiktionary"}
		for _, sn := range rec.Senses {
			if hasAny(sn.Tags, skipTags) || len(sn.Glosses) == 0 {
				continue
			}
			es := entry.Sense{POS: normal.POS, Gloss: strings.Join(sn.Glosses, "; ")}
			for _, ex := range sn.Examples {
				if t := strings.TrimSpace(ex.Text); t != "" {
					es.Examples = append(es.Examples, t)
				}
			}
			if hasAny(sn.Tags, slangTags) {
				slang.Senses = append(slang.Senses, es)
			} else {
				normal.Senses = append(normal.Senses, es)
			}
		}
		for _, e := range []entry.Entry{normal, slang} {
			if len(e.Senses) > 0 {
				if err := emit(e); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Each decodes every line of a (possibly gzipped) wiktextract JSONL file.
func Each(ctx context.Context, r io.Reader, fn func(Record) error) error {
	br := bufio.NewReaderSize(r, 1<<20)
	if magic, _ := br.Peek(2); len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return err
		}
		defer func() { _ = gz.Close() }()
		br = bufio.NewReaderSize(gz, 1<<20)
	}
	sc := bufio.NewScanner(br)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for line := 1; sc.Scan(); line++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		var rec Record
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			return fmt.Errorf("wiktionary line %d: %w", line, err)
		}
		if err := fn(rec); err != nil {
			return err
		}
	}
	return sc.Err()
}

func hasAny(tags, want []string) bool {
	for _, t := range tags {
		if slices.Contains(want, t) {
			return true
		}
	}
	return false
}
