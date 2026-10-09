// Package oewn reads Open English WordNet (WN-LMF XML), the English
// definitions source (spec §2.2).
package oewn

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"time"

	"playword/internal/adapters/sources/fetch"
	"playword/internal/domain/entry"
	"playword/internal/ports"
)

// DefaultURL is the 2025 edition release.
const DefaultURL = "https://github.com/globalwordnet/english-wordnet/releases/download/2025-edition/english-wordnet-2025.xml.gz"

// Source is Open English WordNet as a ports.Source.
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

// Name is "oewn".
func (s *Source) Name() string { return "oewn" }

// Kind is a dictionary.
func (s *Source) Kind() ports.SourceKind { return ports.KindDictionary }

// Lang is English.
func (s *Source) Lang() entry.Lang { return entry.EN }

// URL is where Fetch downloads from.
func (s *Source) URL() string { return s.url }

// License states the data licence.
func (s *Source) License() string {
	return "CC BY 4.0 — Open English WordNet (derived from Princeton WordNet, WordNet License)"
}

// Fetch downloads the release.
func (s *Source) Fetch(ctx context.Context, since time.Time) (io.ReadCloser, ports.Meta, error) {
	return fetch.Get(ctx, s.client, s.url, since)
}

var posNames = map[string]string{"n": "noun", "v": "verb", "a": "adjective", "s": "adjective", "r": "adverb"}

type senseRef struct{ pos, synset string }

type synset struct {
	def      string
	examples []string
}

// Parse emits one entry per lemma, with a sense per synset it belongs to.
// The file lists lexical entries before synsets, so it is read whole first.
// Gzip input is detected and decompressed.
func (s *Source) Parse(ctx context.Context, r io.Reader, emit func(entry.Entry) error) error {
	br := bufio.NewReader(r)
	if magic, _ := br.Peek(2); len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return err
		}
		defer func() { _ = gz.Close() }()
		r = gz
	} else {
		r = br
	}

	var order []string
	lemmas := map[string][]senseRef{}
	synsets := map[string]*synset{}
	dec := xml.NewDecoder(r)
	var lemma, lemmaPOS string
	var cur *synset
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("oewn: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "Lemma":
				lemma, lemmaPOS = attr(t, "writtenForm"), attr(t, "partOfSpeech")
				if _, seen := lemmas[lemma]; !seen {
					order = append(order, lemma)
					lemmas[lemma] = nil
				}
			case "Sense":
				if lemma != "" {
					lemmas[lemma] = append(lemmas[lemma], senseRef{lemmaPOS, attr(t, "synset")})
				}
			case "Synset":
				cur = &synset{}
				synsets[attr(t, "id")] = cur
			case "Definition", "Example":
				var text string
				if err := dec.DecodeElement(&text, &t); err != nil {
					return fmt.Errorf("oewn: %w", err)
				}
				if cur != nil {
					if t.Name.Local == "Definition" && cur.def == "" {
						cur.def = text
					} else if t.Name.Local == "Example" {
						cur.examples = append(cur.examples, text)
					}
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "LexicalEntry":
				lemma = ""
			case "Synset":
				cur = nil
			}
		}
	}

	for _, l := range order {
		e := entry.Entry{Headword: l, Lang: entry.EN, SourceRef: "oewn:" + l}
		for _, ref := range lemmas[l] {
			ss := synsets[ref.synset]
			if ss == nil || ss.def == "" {
				continue
			}
			e.Senses = append(e.Senses, entry.Sense{POS: posNames[ref.pos], Gloss: ss.def, Examples: ss.examples})
		}
		if len(e.Senses) > 0 {
			e.POS = e.Senses[0].POS
		}
		if err := emit(e); err != nil {
			return err
		}
	}
	return nil
}

func attr(t xml.StartElement, name string) string {
	for _, a := range t.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
