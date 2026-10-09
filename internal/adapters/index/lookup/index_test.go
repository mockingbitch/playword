package lookup

import (
	"fmt"

	"math/rand/v2"
	"playword/internal/domain/entry"
	"testing"
)

func TestNormalizeForms(t *testing.T) {
	decomposed := "nhà" // "nhà" typed as a + combining grave
	cases := []struct {
		fn       func(string) string
		in, want string
	}{
		{Normalize, "  Nhà   Cửa ", "nhà cửa"},
		{Normalize, decomposed, "nhà"},
		{Fold, "Đường phố", "duong pho"},
		{Fold, decomposed, "nha"},
		{Telex, "nhà", "nhaf"},
		{Telex, "đường", "dduwowngf"},
		{Telex, "tiếng Việt", "tieengs vieetj"},
		{Telex, "ăn", "awn"},
	}
	for _, c := range cases {
		if got := c.fn(c.in); got != c.want {
			t.Errorf("%q -> %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"hello", "hello", 0}, {"helo", "hello", 1}, {"hlelo", "hello", 1},
		{"hallo", "hello", 1}, {"hxllx", "hello", 2}, {"", "abc", 3},
	}
	for _, c := range cases {
		if got := distance([]rune(c.a), []rune(c.b)); got != c.want {
			t.Errorf("distance(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func testIndex() *Index {
	return NewIndex([]Entry{
		{Word: "nhà", Freq: 90, Senses: []entry.Sense{{Gloss: "house, home"}}},
		{Word: "nhá", Freq: 10, Senses: []entry.Sense{{Gloss: "particle"}}},
		{Word: "nhanh", Freq: 70, Senses: []entry.Sense{{Gloss: "fast"}}},
		{Word: "đường", Freq: 80, Senses: []entry.Sense{{Gloss: "road; sugar"}}},
		{Word: "hello", Freq: 80, Senses: []entry.Sense{{Gloss: "xin chào"}}},
		{Word: "help", Freq: 95, Senses: []entry.Sense{{Gloss: "giúp đỡ"}}},
		{Word: "helium", Freq: 5, Senses: []entry.Sense{{Gloss: "khí heli"}}},
		{Word: "house", Freq: 85, Senses: []entry.Sense{{Gloss: "ngôi nhà, căn nhà"}}},
	})
}

func words(s []Suggestion) []string {
	out := make([]string, len(s))
	for i, x := range s {
		out[i] = x.Entry.Word
	}
	return out
}

func TestSuggest(t *testing.T) {
	idx := testIndex()
	cases := []struct {
		q     string
		first string
	}{
		{"nhà", "nhà"},     // exact beats everything
		{"nha", "nhà"},     // no diacritics: frequency decides between nhà / nhá
		{"nhaf", "nhà"},    // telex
		{"nhá", "nhá"},     // exact with the other tone
		{"duong", "đường"}, // đ folds to d
		{"dduwowngf", "đường"},
		{"hel", "help"},    // prefix ranked by frequency
		{"HELLO", "hello"}, // case-insensitive
		{"helo", "hello"},  // typo
		{"hlelo", "hello"}, // transposition
	}
	for _, c := range cases {
		got := idx.Suggest(c.q, 5)
		if len(got) == 0 || got[0].Entry.Word != c.first {
			t.Errorf("Suggest(%q) = %v, want %q first", c.q, words(got), c.first)
		}
	}
	if got := idx.Suggest("zzz", 5); len(got) != 0 {
		t.Errorf("Suggest(zzz) = %v, want none", words(got))
	}
	if got := idx.Suggest("hel", 2); len(got) != 2 {
		t.Errorf("limit not applied: %v", words(got))
	}
}

func TestLookupAndMeanings(t *testing.T) {
	idx := testIndex()
	if e := idx.Lookup("NHÀ"); e == nil || e.Word != "nhà" {
		t.Errorf("Lookup decomposed nhà = %v", e)
	}
	if e := idx.Lookup("nha"); e != nil {
		t.Errorf("Lookup must be exact, got %q", e.Word)
	}
	got := idx.SearchMeanings("ngoi nha", 5)
	if len(got) != 1 || got[0].Word != "house" {
		t.Errorf("SearchMeanings(ngoi nha) = %v", got)
	}
}

func syntheticIndex(n int) *Index {
	r := rand.New(rand.NewPCG(1, 2))
	const letters = "abcdeghiklmnopqrstuvxyàáảãạăâđêôơư"
	alphabet := []rune(letters)
	entries := make([]Entry, n)
	for i := range entries {
		w := make([]rune, 3+r.IntN(8))
		for j := range w {
			w[j] = alphabet[r.IntN(len(alphabet))]
		}
		entries[i] = Entry{
			Word:   string(w),
			Freq:   r.Float64() * 100,
			Senses: []entry.Sense{{Gloss: fmt.Sprintf("nghĩa số %d của từ", i)}},
		}
	}
	return NewIndex(entries)
}

func BenchmarkSuggest(b *testing.B) {
	idx := syntheticIndex(200_000)
	for _, q := range []string{"a", "nh", "dươ", "abcdeg"} {
		b.Run(q, func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					idx.Suggest(q, 10)
				}
			})
		})
	}
}
