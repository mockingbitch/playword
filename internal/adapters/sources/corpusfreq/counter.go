// Package corpusfreq counts how often dictionary phrases (1–5 syllables)
// occur in a plain-text corpus and turns the counts into Zipf frequencies
// (ADR-003 D2). Only phrases in the vocabulary are counted, so memory stays
// proportional to the dictionary, not the corpus.
package corpusfreq

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// MaxN is the longest phrase counted, matching rhyme.MaxSyl.
const MaxN = 5

type gram [MaxN]uint16 // syllable ids, zero-padded; id 0 means "not in vocabulary"

// Counter counts occurrences of a fixed vocabulary of phrases.
type Counter struct {
	sylID   map[string]uint16
	gramIdx map[gram]int
	phrases []string
}

// NewCounter prepares counting for vocab (normalized phrases). Phrases
// longer than MaxN syllables are ignored.
func NewCounter(vocab []string) (*Counter, error) {
	c := &Counter{sylID: map[string]uint16{}, gramIdx: map[gram]int{}}
	for _, p := range vocab {
		words := strings.Fields(p)
		if len(words) == 0 || len(words) > MaxN {
			continue
		}
		var g gram
		for i, w := range words {
			id, ok := c.sylID[w]
			if !ok {
				if len(c.sylID) == math.MaxUint16 {
					return nil, fmt.Errorf("corpusfreq: more than %d distinct syllables", math.MaxUint16)
				}
				id = uint16(len(c.sylID) + 1)
				c.sylID[w] = id
			}
			g[i] = id
		}
		if _, dup := c.gramIdx[g]; !dup {
			c.gramIdx[g] = len(c.phrases)
			c.phrases = append(c.phrases, p)
		}
	}
	return c, nil
}

// Result is the outcome of one Count.
type Result struct {
	Tokens int64            // syllable tokens in the corpus
	Counts map[string]int64 // phrase -> occurrences (only phrases seen)
}

// Zipf converts a count to the Zipf scale: log10 of occurrences per billion
// tokens (wordfreq's convention; 3 ≈ once per million).
func (r Result) Zipf(count int64) float64 {
	if count <= 0 || r.Tokens == 0 {
		return 0
	}
	return math.Log10(float64(count) * 1e9 / float64(r.Tokens))
}

// Count reads r line by line on `workers` goroutines. An n-gram never
// spans punctuation, digits or a word outside the vocabulary.
func (c *Counter) Count(ctx context.Context, r io.Reader, workers int) (Result, error) {
	workers = max(workers, 1)
	lines := make(chan string, 4*workers)
	type partial struct {
		tokens int64
		counts []int64
	}
	parts := make([]partial, workers)
	var wg sync.WaitGroup
	for w := range workers {
		parts[w].counts = make([]int64, len(c.phrases))
		wg.Add(1)
		go func(p *partial) {
			defer wg.Done()
			for line := range lines {
				p.tokens += c.countLine(line, p.counts)
			}
		}(&parts[w])
	}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	var err error
	for sc.Scan() {
		if err = ctx.Err(); err != nil {
			break
		}
		lines <- sc.Text()
	}
	close(lines)
	wg.Wait()
	if err == nil {
		err = sc.Err()
	}
	if err != nil {
		return Result{}, err
	}

	res := Result{Counts: map[string]int64{}}
	for _, p := range parts {
		res.Tokens += p.tokens
		for i, n := range p.counts {
			if n > 0 {
				res.Counts[c.phrases[i]] += n
			}
		}
	}
	return res, nil
}

// countLine adds the vocabulary n-grams of one line to counts and returns
// the number of syllable tokens it held.
func (c *Counter) countLine(line string, counts []int64) int64 {
	var tokens int64
	var window []uint16
	var word strings.Builder
	flush := func(reset bool) {
		if word.Len() > 0 {
			tokens++
			id := c.sylID[word.String()]
			word.Reset()
			if id == 0 {
				window = window[:0]
			} else {
				if len(window) == MaxN {
					copy(window, window[1:])
					window = window[:MaxN-1]
				}
				window = append(window, id)
				for n := 1; n <= len(window); n++ {
					var g gram
					copy(g[:], window[len(window)-n:])
					if i, ok := c.gramIdx[g]; ok {
						counts[i]++
					}
				}
			}
		}
		if reset {
			window = window[:0]
		}
	}
	for _, r := range norm.NFC.String(strings.ToLower(line)) {
		switch {
		case unicode.IsLetter(r) || unicode.Is(unicode.Mn, r):
			word.WriteRune(r)
		case unicode.IsSpace(r):
			flush(false)
		default: // punctuation, digits, symbols end a phrase
			flush(true)
		}
	}
	flush(true)
	return tokens
}
