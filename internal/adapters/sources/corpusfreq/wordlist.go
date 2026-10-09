package corpusfreq

import (
	"bufio"
	"io"
	"math"
	"strconv"
	"strings"
)

// WordListZipf reads a "word count" frequency list (one word per line, as
// published by FrequencyWords) and returns the Zipf value of each word.
func WordListZipf(r io.Reader) (map[string]float64, error) {
	counts := map[string]int64{}
	var total int64
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 2 {
			continue
		}
		n, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil || n <= 0 {
			continue
		}
		counts[strings.ToLower(f[0])] += n
		total += n
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	out := make(map[string]float64, len(counts))
	for w, n := range counts {
		out[w] = (Result{Tokens: total}).Zipf(n)
	}
	return out, nil
}

// PhraseZipf estimates a phrase's frequency from word frequencies: a single
// word keeps its value; a multiword phrase gets its rarest word's value
// minus one (a phrase is rarer than its parts). Phrases with an unknown word
// get ok=false.
func PhraseZipf(phrase string, words map[string]float64) (float64, bool) {
	parts := strings.Fields(strings.ToLower(phrase))
	if len(parts) == 0 {
		return 0, false
	}
	low := math.Inf(1)
	for _, p := range parts {
		z, ok := words[p]
		if !ok {
			return 0, false
		}
		low = min(low, z)
	}
	if len(parts) > 1 {
		low = max(low-1, 0)
	}
	return low, true
}
