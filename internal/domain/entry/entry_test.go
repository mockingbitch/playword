package entry

import (
	"bytes"
	"testing"
)

func TestNormalize(t *testing.T) {
	if got := Normalize("  May   Mắn "); got != "may mắn" {
		t.Errorf("Normalize = %q", got)
	}
}

func TestHasDefinition(t *testing.T) {
	if (&Entry{Senses: []Sense{{Gloss: "  "}}}).HasDefinition() {
		t.Error("blank gloss is no definition")
	}
	if !(&Entry{Senses: []Sense{{}, {Gloss: "Gặp dịp tốt."}}}).HasDefinition() {
		t.Error("one gloss is enough")
	}
}

func TestContentHash(t *testing.T) {
	a := Entry{Headword: "may mắn", Senses: []Sense{{Gloss: "Gặp dịp tốt."}}}
	b := a
	if !bytes.Equal(a.ContentHash(), b.ContentHash()) {
		t.Error("equal entries must hash equal")
	}
	b.Tags = []string{"proper_noun"}
	if bytes.Equal(a.ContentHash(), b.ContentHash()) {
		t.Error("tags must change the hash")
	}
	b = a
	b.SourceRef = "elsewhere#L2"
	if !bytes.Equal(a.ContentHash(), b.ContentHash()) {
		t.Error("source position alone must not count as a change")
	}
}
