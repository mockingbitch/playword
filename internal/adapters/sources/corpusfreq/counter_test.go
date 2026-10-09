package corpusfreq

import (
	"context"
	"math"
	"strings"
	"testing"
)

func TestCount(t *testing.T) {
	c, err := NewCounter([]string{"may mắn", "may", "mắn", "ngày tàn", "tàn", "một hai ba bốn năm sáu", "may mắn"})
	if err != nil {
		t.Fatal(err)
	}
	corpus := strings.Join([]string{
		"Thật May MẮN khi gặp lại, may mắn thay!",
		"may. mắn",          // punctuation breaks the phrase
		"may xyz mắn",       // so does a word outside the vocabulary
		"ngày tàn 2026 tàn", // and digits
		"Mắn",             // decomposed input
	}, "\n")
	for _, workers := range []int{1, 4} {
		res, err := c.Count(context.Background(), strings.NewReader(corpus), workers)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]int64{"may mắn": 2, "may": 4, "mắn": 5, "ngày tàn": 1, "tàn": 2}
		for p, n := range want {
			if res.Counts[p] != n {
				t.Errorf("workers=%d %q = %d, want %d", workers, p, res.Counts[p], n)
			}
		}
		if res.Tokens != 18 { // 9 + 2 + 3 + 3 + 1
			t.Errorf("tokens = %d", res.Tokens)
		}
		if _, ok := res.Counts["một hai ba bốn năm sáu"]; ok {
			t.Error("phrases longer than 5 syllables are not counted")
		}
	}
}

func TestZipf(t *testing.T) {
	r := Result{Tokens: 1_000_000}
	if z := r.Zipf(1); math.Abs(z-3) > 1e-9 {
		t.Errorf("once per million = %v, want 3", z)
	}
	if r.Zipf(0) != 0 || (Result{}).Zipf(5) != 0 {
		t.Error("zero cases")
	}
}

func TestCountCancelled(t *testing.T) {
	c, _ := NewCounter([]string{"a"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Count(ctx, strings.NewReader("a\na\n"), 2); err == nil {
		t.Error("cancelled context must fail")
	}
}

func TestWordList(t *testing.T) {
	z, err := WordListZipf(strings.NewReader("money 900\nhoney 90\nbad line\nzero 0\nmoney 9\nthe x\n"))
	if err != nil {
		t.Fatal(err)
	}
	// total 999: money 909/999 per token.
	if math.Abs(z["money"]-math.Log10(909*1e9/999)) > 1e-9 || len(z) != 2 {
		t.Errorf("zipf = %v", z)
	}
	if v, ok := PhraseZipf("Honey", z); !ok || v != z["honey"] {
		t.Error("single word keeps its value")
	}
	if v, ok := PhraseZipf("money honey", z); !ok || math.Abs(v-(z["honey"]-1)) > 1e-9 {
		t.Errorf("phrase = %v", v)
	}
	if _, ok := PhraseZipf("money tree", z); ok {
		t.Error("unknown word")
	}
	if _, ok := PhraseZipf(" ", z); ok {
		t.Error("empty")
	}
}
