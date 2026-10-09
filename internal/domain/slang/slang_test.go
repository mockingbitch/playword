package slang

import (
	"errors"
	"strings"
	"testing"

	"playword/internal/domain/entry"
)

func TestValidate(t *testing.T) {
	s := Submission{Headword: "  ét   o ét ", Gloss: " cứu ", Respelling: " ét  o ét ", Tags: []string{" GenZ "}}
	if err := s.Validate(); err != nil || s.Headword != "ét o ét" || s.Lang != entry.VI || s.Gloss != "cứu" || s.Tags[0] != "genz" {
		t.Fatalf("%+v %v", s, err)
	}
	bad := []Submission{
		{Headword: "x", Gloss: "y", Lang: "fr"},
		{Headword: "", Gloss: "y"},
		{Headword: strings.Repeat("a", 61), Gloss: "y"},
		{Headword: "a b c d e f", Gloss: "y"},
		{Headword: "x", Gloss: ""},
		{Headword: "x", Gloss: strings.Repeat("y", 501)},
		{Headword: "x", Gloss: "y", Example: strings.Repeat("z", 501)},
		{Headword: "x", Gloss: "y", Respelling: strings.Repeat("z", 61)},
		{Headword: "x", Gloss: "y", Tags: make([]string, 11)},
	}
	for _, b := range bad {
		if err := b.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v", b, err)
		}
	}
}

func TestEntry(t *testing.T) {
	s := Submission{ID: 7, Headword: "chill", Lang: entry.VI, Gloss: "thư giãn", Example: "chill thôi", Respelling: "chiu", Tags: []string{"genz"}}
	e := s.Entry()
	if !e.IsSlang || e.Respelling != "chiu" || e.Tags[0] != "slang" || e.Tags[1] != "genz" ||
		e.Senses[0].Examples[0] != "chill thôi" || e.SourceRef != "slang:7" {
		t.Errorf("%+v", e)
	}
	if e := (&Submission{Headword: "x", Gloss: "y"}).Entry(); e.Senses[0].Examples != nil {
		t.Error("no example, no examples")
	}
}
