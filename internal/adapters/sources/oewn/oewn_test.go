package oewn

import (
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"testing"

	"playword/internal/domain/entry"
)

func parse(t *testing.T, data []byte) map[string]entry.Entry {
	t.Helper()
	got := map[string]entry.Entry{}
	err := New("", nil).Parse(context.Background(), bytes.NewReader(data), func(e entry.Entry) error {
		got[e.Headword] = e
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestParse(t *testing.T) {
	raw, err := os.ReadFile("testdata/mini.xml")
	if err != nil {
		t.Fatal(err)
	}
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	_, _ = w.Write(raw)
	_ = w.Close()

	for name, data := range map[string][]byte{"xml": raw, "gzip": gz.Bytes()} {
		got := parse(t, data)
		money := got["money"]
		if len(money.Senses) != 2 || money.POS != "noun" || money.Senses[0].Examples[0] != "he has money to burn" || money.Lang != entry.EN {
			t.Errorf("%s: money = %+v", name, money)
		}
		honey := got["honey"]
		if len(honey.Senses) != 2 || honey.Senses[1].POS != "verb" {
			t.Errorf("%s: honey (noun + verb) = %+v", name, honey)
		}
		if got["kick the bucket"].Senses[0].Gloss != "die" {
			t.Errorf("%s: multiword lemma = %+v", name, got["kick the bucket"])
		}
		if o := got["orphan"]; len(o.Senses) != 0 {
			t.Errorf("%s: a sense whose synset is missing has no gloss: %+v", name, o)
		}
	}
	s := New("", nil)
	if s.Name() != "oewn" || s.Kind() != "dictionary" || s.Lang() != entry.EN || s.URL() != DefaultURL || s.License() == "" {
		t.Error("metadata")
	}
	if err := s.Parse(context.Background(), bytes.NewReader([]byte("<a><b></a>")), func(entry.Entry) error { return nil }); err == nil {
		t.Error("broken XML must fail")
	}
}
