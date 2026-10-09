// Package hnd reads the Vietnamese–Vietnamese dictionary of Hồ Ngọc Đức
// (Free Vietnamese Dictionary Project) in the StarDict tab format, as
// repackaged by github.com/catusf/tudien.
//
// Format: "##key<TAB>value" header lines, then "headword<TAB>html". Words
// that differ only in tone marks share one line, each extra word introduced
// by "<hr/> <h3>word</h3>"; senses are separated by <br>.
package hnd

import (
	"bufio"
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"playword/internal/adapters/sources/fetch"
	"playword/internal/domain/entry"
	"playword/internal/ports"
)

// DefaultURL is the repackaged dump in StarDict tab format.
const DefaultURL = "https://raw.githubusercontent.com/catusf/tudien/master/dict/star_vietviet.tab"

// Source is the HND dictionary as a ports.Source.
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

// Name is "hnd".
func (s *Source) Name() string { return "hnd" }

// Kind is a dictionary: it emits entries with definitions.
func (s *Source) Kind() ports.SourceKind { return ports.KindDictionary }

// Lang is Vietnamese.
func (s *Source) Lang() entry.Lang { return entry.VI }

// URL is where Fetch downloads from.
func (s *Source) URL() string { return s.url }

// License states the data licence shown in the README and on pages.
func (s *Source) License() string {
	return "GPL — Free Vietnamese Dictionary Project (Hồ Ngọc Đức); repackaged by catusf/tudien (CC0)"
}

// Fetch downloads the dump; with a non-zero since it asks the server to skip
// an unchanged file.
func (s *Source) Fetch(ctx context.Context, since time.Time) (io.ReadCloser, ports.Meta, error) {
	return fetch.Get(ctx, s.client, s.url, since)
}

var (
	reSection = regexp.MustCompile(`<hr\s*/?>`)
	reH3      = regexp.MustCompile(`^\s*<h3>(.*?)</h3>`)
	reHidden  = regexp.MustCompile(`<font color="white">.*?</font>|<font[^>]*$`) // unaccented search aid
	reBreak   = regexp.MustCompile(`<br\s*/?>|\s//\s`)
	reTag     = regexp.MustCompile(`<[^>]*>`)
	reLead    = regexp.MustCompile(`^-?\s*(?:\d+\s+)?`)             // "- 2 "
	reNumber  = regexp.MustCompile(`(?:^|\s)(\d{1,2})\.?\s+\p{Lu}`) // "1. Công trình", "2 Quyết định"
	rePOS     = regexp.MustCompile(`^(\p{L}{1,4})(\.?)\s+`)         // "dt. ", or "dt (" before an etymology
)

// Part-of-speech abbreviations used by the dictionary. Others that look
// alike (Nh. = như, Cg. = cũng gọi, X. = xem) are part of the definition.
var posNames = map[string]string{
	"d": "danh từ", "dt": "danh từ",
	"đg": "động từ", "đgt": "động từ",
	"t": "tính từ", "tt": "tính từ",
	"p": "phụ từ", "ph": "phụ từ", "pht": "phụ từ",
	"trgt": "trạng từ", "đt": "đại từ",
	"k": "kết từ", "lt": "liên từ", "gt": "giới từ", "trt": "trợ từ",
	"c": "cảm từ", "tht": "thán từ",
}

// Parse emits one entry per headword. Lines it cannot read are emitted
// anyway; the pipeline's validation decides what to skip and why.
func (s *Source) Parse(ctx context.Context, r io.Reader, emit func(entry.Entry) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for line := 1; sc.Scan(); line++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		text := sc.Text()
		if strings.HasPrefix(text, "##") || strings.TrimSpace(text) == "" {
			continue
		}
		head, body, ok := strings.Cut(text, "\t")
		if !ok {
			continue
		}
		// The original FVDP format marked headwords with "@"; one survives
		// after the byte-order mark on the first line.
		head = strings.TrimLeft(strings.TrimPrefix(head, "\ufeff"), "@")
		for i, section := range reSection.Split(body, -1) {
			word := head
			if i > 0 {
				m := reH3.FindStringSubmatch(section)
				if m == nil {
					continue
				}
				word, section = m[1], section[len(m[0]):]
			}
			e := entry.Entry{
				Headword:  strings.TrimSpace(html.UnescapeString(reTag.ReplaceAllString(word, ""))),
				Lang:      entry.VI,
				Senses:    parseSenses(section),
				SourceRef: fmt.Sprintf("%s#L%d", s.url, line),
			}
			for _, sn := range e.Senses {
				if sn.POS != "" {
					e.POS = sn.POS
					break
				}
			}
			if err := emit(e); err != nil {
				return err
			}
		}
	}
	return sc.Err()
}

func parseSenses(section string) []entry.Sense {
	section = reHidden.ReplaceAllString(section, "")
	var out []entry.Sense
	pos := ""
	for _, part := range reBreak.Split(section, -1) {
		text := strings.Join(strings.Fields(html.UnescapeString(reTag.ReplaceAllString(part, " "))), " ")
		text = reLead.ReplaceAllString(text, "")
		if m := rePOS.FindStringSubmatch(text); m != nil {
			rest := text[len(m[0]):]
			if name, ok := posNames[m[1]]; ok && (m[2] == "." || strings.HasPrefix(rest, "(")) {
				pos, text = name, rest
			}
		}
		// "(H. ai: thương xót; oán: hờn giận) Đau thương…": the Sino-Vietnamese
		// etymology moves behind the definition.
		etym := ""
		if strings.HasPrefix(text, "(H. ") {
			if end := closingParen(text); end > 0 {
				etym, text = text[:end+1], strings.TrimSpace(text[end+1:])
			}
		}
		if text == "" && etym == "" {
			continue
		}
		for i, sub := range splitNumbered(text) {
			sense := entry.Sense{POS: pos, Gloss: sub}
			// "nghĩa: ví dụ" — usage examples follow a colon outside parentheses.
			if c := topLevelColon(sub); c > 0 && strings.TrimSpace(sub[c+1:]) != "" {
				sense.Gloss, sense.Examples = sub[:c], []string{strings.TrimSpace(sub[c+1:])}
			}
			if i == 0 {
				sense.Gloss = strings.TrimSpace(sense.Gloss + " " + etym)
			}
			out = append(out, sense)
		}
	}
	return out
}

// closingParen returns the index of the parenthesis closing the one at s[0].
func closingParen(s string) int {
	depth := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return -1
}

// topLevelColon returns the index of the first ": " outside parentheses.
func topLevelColon(s string) int {
	depth := 0
	for i, r := range s {
		switch {
		case r == '(':
			depth++
		case r == ')' && depth > 0:
			depth--
		case r == ':' && depth == 0 && strings.HasPrefix(s[i:], ": "):
			return i
		}
	}
	return -1
}

// splitNumbered splits "1. Nghĩa một 2. Nghĩa hai" into sub-senses. Only a
// run counting up from 1 with a capital letter after each number counts,
// so numbers inside a definition ("2 người") are left alone.
func splitNumbered(text string) []string {
	var cuts []int // start of each numbered item, number included
	want := 1
	for _, m := range reNumber.FindAllStringSubmatchIndex(text, -1) {
		if n, _ := strconv.Atoi(text[m[2]:m[3]]); n == want {
			cuts = append(cuts, m[2])
			want++
		}
	}
	if len(cuts) < 2 || strings.TrimSpace(text[:cuts[0]]) != "" {
		return []string{text}
	}
	out := make([]string, len(cuts))
	for i := range cuts {
		end := len(text)
		if i+1 < len(cuts) {
			end = cuts[i+1]
		}
		item := text[cuts[i]:end]
		// drop the leading "N." / "N "
		item = strings.TrimLeft(item, "0123456789")
		item = strings.TrimPrefix(item, ".")
		out[i] = strings.TrimSpace(item)
	}
	return out
}
