package app

import (
	"errors"
	"strings"
	"testing"

	"playword/internal/app/apptest"
	"playword/internal/domain/account"
	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme"
	"playword/internal/domain/slang"
	"playword/internal/ingest"
)

func newSlang(t *testing.T) (*Slang, *apptest.Slang, *apptest.Ingest) {
	t.Helper()
	e, err := rhyme.LoadDir("../../configs")
	if err != nil {
		t.Fatal(err)
	}
	ing := apptest.NewIngest()
	store := apptest.NewSlang(ing)
	return NewSlang(store, ingest.Pipeline{Store: ing, Engine: e}, nil), store, ing
}

func TestSlangReview(t *testing.T) {
	s, store, ing := newSlang(t)
	bad := []slang.Submission{
		{Headword: "", Gloss: "x"},
		{Headword: "chill", Gloss: ""},
		{Headword: "chill", Gloss: "x", Lang: "fr"},
		{Headword: "một hai ba bốn năm sáu", Gloss: "x"},
		{Headword: strings.Repeat("a", 61), Gloss: "x"},
		{Headword: "x", Gloss: "x", Tags: make([]string, 11)},
	}
	for _, b := range bad {
		if err := s.Submit(ctx, &b); !errors.Is(err, slang.ErrInvalid) {
			t.Errorf("%+v: %v", b, err)
		}
	}
	chill := &slang.Submission{Headword: "  chill ", Gloss: "thư giãn", Respelling: "chiu", Example: "chill thôi", Tags: []string{" GenZ "}}
	if err := s.Submit(ctx, chill); err != nil || chill.Lang != entry.VI || chill.Headword != "chill" || chill.Tags[0] != "genz" {
		t.Fatalf("%+v %v", chill, err)
	}
	if err := s.Submit(ctx, &slang.Submission{Headword: "ét o ét", Gloss: "cứu"}); err != nil {
		t.Fatal(err)
	}
	pending, _ := s.List(ctx, "", 10)
	if len(pending) != 2 {
		t.Fatalf("pending = %d", len(pending))
	}
	if _, err := s.List(ctx, "bogus", 10); !errors.Is(err, slang.ErrInvalid) {
		t.Error("bad status")
	}

	sub, err := s.Approve(ctx, "admin@example.vn", chill.ID)
	if err != nil || sub.EntryID == 0 || store.Trends[sub.EntryID] != 1 || store.Notices != 1 {
		t.Fatalf("approve: %+v %v", sub, err)
	}
	w := ing.Written[0]
	if !w.Entry.IsSlang || w.Entry.Respelling != "chiu" || len(w.Syllables) != 1 || w.Syllables[0].Nucleus != "I" || w.Entry.Tags[0] != "slang" {
		t.Errorf("written = %+v (chill is keyed by its respelling chiu)", w)
	}
	if _, err := s.Approve(ctx, "admin", chill.ID); !errors.Is(err, slang.ErrInvalid) {
		t.Error("approving twice")
	}
	if err := s.Reject(ctx, "admin", 2); err != nil {
		t.Fatal(err)
	}
	if err := s.Reject(ctx, "admin", 2); !errors.Is(err, slang.ErrInvalid) {
		t.Errorf("rejecting twice: %v", err)
	}
	if err := s.Reject(ctx, "admin", 99); !errors.Is(err, account.ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
	if strings.Join(store.Statuses(), ",") != "approved,rejected" {
		t.Errorf("statuses = %v", store.Statuses())
	}

	if v, err := s.Bump(ctx, sub.EntryID, 0.2); err != nil || v != 1.2 {
		t.Errorf("bump: %v %v", v, err)
	}
	if _, err := s.Bump(ctx, sub.EntryID, 50); !errors.Is(err, slang.ErrInvalid) {
		t.Error("bump bound")
	}
}

func TestImportCSV(t *testing.T) {
	s, _, _ := newSlang(t)
	csvData := "\ufeffheadword,lang,gloss,example,respelling,tags\n" +
		"flex,vi,khoe khoang,flex nhẹ,phờ-léc,genz;rap\n" +
		",vi,thiếu từ,,,\n" +
		"\"quote\nbroken,vi,x\n"
	n, errs := s.ImportCSV(ctx, strings.NewReader(csvData), "csv:test")
	if n != 1 || len(errs) == 0 {
		t.Errorf("n=%d errs=%v", n, errs)
	}
	pending, _ := s.List(ctx, "pending", 10)
	if len(pending) != 1 || pending[0].Respelling != "phờ-léc" || len(pending[0].Tags) != 2 || pending[0].SubmittedBy != "csv:test" {
		t.Errorf("%+v", pending)
	}
	if n, errs := s.ImportCSV(ctx, strings.NewReader("word,meaning\nx,y\n"), "csv"); n != 0 || len(errs) != 1 {
		t.Error("header must name headword")
	}
	if _, errs := s.ImportCSV(ctx, strings.NewReader(""), "csv"); len(errs) != 1 {
		t.Error("empty file")
	}
}
