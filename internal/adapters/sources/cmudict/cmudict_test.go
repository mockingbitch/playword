package cmudict

import (
	"context"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	in := "MONEY M AH1 N IY0\nread R EH1 D\nread(2) R IY1 D\nhoney HH AH1 N IY0 # comment\n\n# only a comment\n"
	got := map[string]string{}
	err := Parse(context.Background(), strings.NewReader(in), func(w string, ph []string) error {
		got[w] = strings.Join(ph, " ")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"money": "M AH1 N IY0", "read": "R EH1 D", "honey": "HH AH1 N IY0"}
	if len(got) != len(want) {
		t.Errorf("got %v", got)
	}
	for w, p := range want {
		if got[w] != p {
			t.Errorf("%s = %q, want %q", w, got[w], p)
		}
	}
	if s := New("", nil); s.Name() != "cmudict" || s.URL() != DefaultURL {
		t.Error("metadata")
	}
}
