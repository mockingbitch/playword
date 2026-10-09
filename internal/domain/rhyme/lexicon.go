package rhyme

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"playword/pkg/vnphon"
)

// ReadLexicon reads the plain fixture format used by tests and playword-cli:
// one phrase per line, then optional tab-separated Zipf frequency and the
// word "slang". Blank lines and lines starting with # are ignored.
func ReadLexicon(r io.Reader) ([]Candidate, error) {
	var out []Candidate
	sc := bufio.NewScanner(r)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Split(text, "\t")
		syl, err := vnphon.ParsePhrase(fields[0])
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		c := Candidate{ID: int64(len(out) + 1), Text: strings.TrimSpace(fields[0]), Syllables: syl}
		for _, f := range fields[1:] {
			f = strings.TrimSpace(f)
			if f == "slang" {
				c.IsSlang = true
			} else if z, err := strconv.ParseFloat(f, 64); err == nil {
				c.Zipf = z
			} else if f != "" {
				return nil, fmt.Errorf("line %d: unexpected field %q", line, f)
			}
		}
		out = append(out, c)
	}
	return out, sc.Err()
}
