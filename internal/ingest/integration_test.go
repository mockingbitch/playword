package ingest_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"playword/internal/adapters/index/rhymeindex"
	"playword/internal/adapters/postgres"
	"playword/internal/app"
	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme"
	"playword/internal/domain/rhyme/en"
	"playword/internal/domain/slang"
	"playword/internal/ingest"
	"playword/internal/ports"
	"playword/pkg/vnphon"
)

// Integration tests need a PostgreSQL server; each run creates and drops
// its own database. With docker compose up:
//
//	PLAYWORD_TEST_DATABASE_URL=postgres://playword:playword@127.0.0.1:55432/playword?sslmode=disable
func testStore(t *testing.T) *postgres.Store {
	t.Helper()
	admin := os.Getenv("PLAYWORD_TEST_DATABASE_URL")
	if admin == "" {
		t.Skip("PLAYWORD_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("playword_it_%d", time.Now().UnixNano())
	if _, err := conn.Exec(ctx, "create database "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "drop database "+name+" with (force)")
		_ = conn.Close(context.Background())
	})
	u, _ := url.Parse(admin)
	u.Path = "/" + name
	st, err := postgres.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.Migrate(ctx, "up"); err != nil {
		t.Fatal(err)
	}
	return st
}

// fakeSource emits a fixed list of entries.
type fakeSource struct{ entries []entry.Entry }

func (f *fakeSource) Name() string           { return "fake" }
func (f *fakeSource) Kind() ports.SourceKind { return ports.KindDictionary }
func (f *fakeSource) Lang() entry.Lang       { return entry.VI }
func (f *fakeSource) License() string        { return "test" }
func (f *fakeSource) URL() string            { return "memory://fake" }
func (f *fakeSource) Fetch(context.Context, time.Time) (io.ReadCloser, ports.Meta, error) {
	return io.NopCloser(strings.NewReader("")), ports.Meta{}, nil
}
func (f *fakeSource) Parse(_ context.Context, _ io.Reader, emit func(entry.Entry) error) error {
	for _, e := range f.entries {
		if err := emit(e); err != nil {
			return err
		}
	}
	return nil
}

func def(word, gloss string) entry.Entry {
	return entry.Entry{Headword: word, Lang: entry.VI, Senses: []entry.Sense{{Gloss: gloss}}}
}

// lexicon builds ~1000 real-shaped entries from the rime inventory plus the
// spec's acceptance words and a few that must be skipped.
func lexicon() []entry.Entry {
	out := []entry.Entry{
		def("may mắn", "Gặp dịp tốt."),
		def("ngày tàn", "Ngày tàn lụi."),
		def("chạy án", "Tìm cách thoát tội."),
		def("ngày mai", "Ngày sau hôm nay."),
		def("Hà Nội", "Thủ đô."),
		def("wifi", "Mạng không dây."),        // kept, no rhyme keys
		def("- Người đời Hán. @Tư Mã:", "x"),  // bad_headword
		{Headword: "trống", Lang: entry.VI},   // no_definition
		def("một hai ba bốn năm sáu", "Đếm."), // too_long
		def("may mắn", "Được may."),           // duplicate: senses merge
	}
	for i, sp := range vnphon.RimeSpellings() {
		for j, onset := range []string{"b", "l", "m", "t", "th", "ch"} {
			if len(out) >= 1000 {
				return out
			}
			w := onset + sp
			if s, err := vnphon.ParseSyllable(w + "́"); err == nil && vnphon.IsStopCoda(s.Coda) {
				w += "́"
			} else if err != nil {
				continue
			}
			out = append(out, def(w, fmt.Sprintf("Từ thử %d-%d.", i, j)))
		}
	}
	return out
}

func engine(t *testing.T, dir string) *rhyme.Engine {
	t.Helper()
	e, err := rhyme.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestIngestIndexSearch(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	e := engine(t, "../../configs")
	src := &fakeSource{entries: lexicon()}
	p := ingest.Pipeline{Store: st, Engine: e, BatchSize: 300}

	// First run inserts everything valid.
	rep, err := p.Run(ctx, src, ingest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Status != "ok" || rep.Inserted < 900 || rep.Updated != 0 || rep.NoRhyme != 1 {
		t.Fatalf("first run: %+v", rep)
	}
	for _, reason := range []string{ingest.SkipBadHeadword, ingest.SkipNoDefinition, ingest.SkipTooLong} {
		if rep.SkipReasons[reason] != 1 {
			t.Errorf("skip %s = %d", reason, rep.SkipReasons[reason])
		}
	}

	// Second run is idempotent.
	rep2, err := p.Run(ctx, src, ingest.Options{})
	if err != nil || rep2.Inserted != 0 || rep2.Updated != 0 || rep2.Unchanged != rep.Inserted {
		t.Fatalf("second run: %+v, %v", rep2, err)
	}

	// The index finds the spec's rhymes.
	var h rhymeindex.Holder
	snap, err := h.Rebuild(ctx, st, e)
	if err != nil {
		t.Fatal(err)
	}
	got := search(t, snap, e, "ngày tàn")
	for _, want := range []string{"may mắn", "chạy án"} {
		if !slices.Contains(got, want) {
			t.Errorf("ngày tàn: missing %q in %v", want, got[:min(10, len(got))])
		}
	}
	if slices.Contains(got, "ngày mai") || slices.Contains(got, "ngày tàn") {
		t.Error("ngày mai / ngày tàn must not be returned")
	}
	if en := snap.Entry(idOf(t, snap, e, "may mắn")); en == nil || len(en.Senses) != 2 {
		t.Errorf("duplicate senses must merge: %+v", en)
	}

	// A dry run reports the change but leaves the data alone.
	changed := &fakeSource{entries: slices.Clone(src.entries)}
	changed.entries[0] = def("may mắn", "Đổi nghĩa.")
	rep3, err := p.Run(ctx, changed, ingest.Options{DryRun: true})
	if err != nil || rep3.Updated != 1 || rep3.Status != "dry_run" {
		t.Fatalf("dry run: %+v, %v", rep3, err)
	}
	if rep4, _ := p.Run(ctx, src, ingest.Options{}); rep4.Updated != 0 {
		t.Errorf("dry run leaked a change: %+v", rep4)
	}

	// Entries missing from a full run become stale and leave the index.
	shrunk := &fakeSource{entries: slices.DeleteFunc(slices.Clone(src.entries), func(e entry.Entry) bool {
		return e.Headword == "may mắn"
	})}
	rep5, err := p.Run(ctx, shrunk, ingest.Options{})
	if err != nil || rep5.Staled != 1 {
		t.Fatalf("stale run: %+v, %v", rep5, err)
	}
	snap, _ = h.Rebuild(ctx, st, e)
	if slices.Contains(search(t, snap, e, "ngày tàn"), "may mắn") {
		t.Error("stale entry still indexed")
	}
	// …and come back when seen again.
	if rep6, _ := p.Run(ctx, src, ingest.Options{}); rep6.Staled != 0 {
		t.Errorf("revive: %+v", rep6)
	}
	snap, _ = h.Rebuild(ctx, st, e)
	if !slices.Contains(search(t, snap, e, "ngày tàn"), "may mắn") {
		t.Error("revived entry missing from index")
	}
	if snap.Gen <= 1 {
		t.Error("generation must grow on every swap")
	}
}

func TestRhymeConfigChange(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	e := engine(t, "../../configs")
	p := ingest.Pipeline{Store: st, Engine: e}
	src := &fakeSource{entries: []entry.Entry{def("ngày tàn", "a"), def("chạy làng", "b")}}
	if _, err := p.Run(ctx, src, ingest.Options{}); err != nil {
		t.Fatal(err)
	}

	// Same rules minus the n~ng cross pair → a different config.
	dir := t.TempDir()
	fam, _ := os.ReadFile("../../configs/rhyme_families.yaml")
	dia, _ := os.ReadFile("../../configs/dialects.yaml")
	fam = []byte(strings.Replace(string(fam), "  - [n, ng]", "", 1))
	for name, b := range map[string][]byte{"rhyme_families.yaml": fam, "dialects.yaml": dia} {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e2 := engine(t, dir)

	p2 := ingest.Pipeline{Store: st, Engine: e2}
	if _, err := p2.Run(ctx, src, ingest.Options{}); !errors.Is(err, ports.ErrKeysConfigMismatch) {
		t.Fatalf("ingest with another config: %v", err)
	}
	var h rhymeindex.Holder
	if _, err := h.Rebuild(ctx, st, e2); !errors.Is(err, ports.ErrKeysConfigMismatch) {
		t.Fatalf("index with another config: %v", err)
	}

	// rebuild-keys switches the stored keys; listeners are notified.
	notified := make(chan error, 1)
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	go func() { notified <- st.WaitForChange(waitCtx) }()
	time.Sleep(200 * time.Millisecond) // let LISTEN register
	n, err := st.RebuildKeys(ctx, e2)
	if err != nil || n == 0 {
		t.Fatalf("rebuild-keys: %d, %v", n, err)
	}
	if err := <-notified; err != nil {
		t.Errorf("notify: %v", err)
	}
	snap, err := h.Rebuild(ctx, st, e2)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(search(t, snap, e2, "ngày tàn"), "chạy làng") {
		t.Error("without n~ng, chạy làng must not match at L2.5")
	}
	if _, err := h.Rebuild(ctx, st, e); !errors.Is(err, ports.ErrKeysConfigMismatch) {
		t.Error("the old config is now the mismatched one")
	}
}

func search(t *testing.T, s *rhymeindex.Snapshot, e *rhyme.Engine, text string) []string {
	t.Helper()
	p, err := vnphon.ParsePhrase(text)
	if err != nil {
		t.Fatal(err)
	}
	q := rhyme.NewQuery(p)
	q.Limit = 1000
	resp, err := s.Search(e, q)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range resp.Results {
		out = append(out, r.Candidate.Text)
	}
	return out
}

func idOf(t *testing.T, s *rhymeindex.Snapshot, e *rhyme.Engine, text string) int64 {
	t.Helper()
	p, _ := vnphon.ParsePhrase(text)
	q := rhyme.NewQuery(p)
	q.Level, q.Limit = rhyme.L3, 5000
	// Search excludes the query itself, so look it up through a rhyme partner.
	partner, _ := vnphon.ParsePhrase("ngày tàn")
	q.Syllables = partner
	resp, _ := s.Search(e, q)
	for _, r := range resp.Results {
		if r.Candidate.Text == text {
			return r.Candidate.ID
		}
	}
	t.Fatalf("%q not in index", text)
	return 0
}

func TestWordFreqRanking(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	e := engine(t, "../../configs")
	src := &fakeSource{entries: []entry.Entry{def("ngày tàn", "a"), def("may mắn", "b"), def("cay đắng", "c"), def("Thái An", "d")}}
	if _, err := (&ingest.Pipeline{Store: st, Engine: e}).Run(ctx, src, ingest.Options{}); err != nil {
		t.Fatal(err)
	}
	vocab, err := st.Vocab(ctx, entry.VI)
	if err != nil || len(vocab) != 4 {
		t.Fatalf("vocab = %v, %v", vocab, err)
	}
	fs := postgres.FreqSource{Name: "test-freq", License: "test", Lang: entry.VI}
	// A dry run leaves no rows behind.
	if _, err := st.ReplaceWordFreq(ctx, fs, []postgres.FreqRow{{Normalized: "cay đắng", Zipf: 8}}, nil, true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReplaceWordFreq(ctx, fs, []postgres.FreqRow{{Normalized: "may mắn", Zipf: 4.4}, {Normalized: "thái an", Zipf: 7}}, nil, false); err != nil {
		t.Fatal(err)
	}
	var h rhymeindex.Holder
	snap, err := h.Rebuild(ctx, st, e)
	if err != nil {
		t.Fatal(err)
	}
	// Frequency lifts may mắn above cay đắng; the more frequent name still comes last.
	if got := search(t, snap, e, "ngày tàn"); strings.Join(got, "|") != "may mắn|cay đắng|Thái An" {
		t.Errorf("order = %v", got)
	}
}

func TestEnglishIngest(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	e := engine(t, "../../configs")
	prons := map[string]string{"money": "M AH1 N IY0", "honey": "HH AH1 N IY0", "tummy": "T AH1 M IY0", "get": "G EH1 T"}
	var rows []postgres.PronRow
	for w, p := range prons {
		rows = append(rows, postgres.PronRow{Normalized: w, Phonemes: en.ParsePhones(p), Confidence: 1})
	}
	if _, err := st.ReplacePronunciations(ctx, postgres.FreqSource{Name: "cmudict", License: "BSD", Lang: entry.EN}, rows, false); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.Pronunciations(ctx, entry.EN)
	if err != nil || len(loaded) != 4 {
		t.Fatalf("pronunciations = %v, %v", loaded, err)
	}
	src := &fakeSource{entries: []entry.Entry{
		{Headword: "money", Lang: entry.EN, Senses: []entry.Sense{{Gloss: "medium of exchange"}}},
		{Headword: "honey", Lang: entry.EN, Senses: []entry.Sense{{Gloss: "sweet"}}},
		{Headword: "tummies", Lang: entry.EN, Senses: []entry.Sense{{Gloss: "bellies"}}},  // derived from tummy
		{Headword: "zyzzyva", Lang: entry.EN, Senses: []entry.Sense{{Gloss: "a weevil"}}}, // no pronunciation
	}}
	p := ingest.Pipeline{Store: st, Engine: e, EN: en.NewPronouncer(loaded)}
	rep, err := p.Run(ctx, src, ingest.Options{})
	if err != nil || rep.Inserted != 4 || rep.NoRhyme != 1 || !rep.KeysRebuilt {
		t.Fatalf("report %+v, %v", rep, err)
	}

	var h rhymeindex.Holder
	snap, err := h.Rebuild(ctx, st, e)
	if err != nil {
		t.Fatal(err)
	}
	segs, conf, err := snap.PronounceEN("money")
	if err != nil || conf != 1 {
		t.Fatal(err)
	}
	q := rhyme.Query{Lang: entry.EN, Segs: segs, Text: "money", Level: rhyme.Family, MinSyl: 1, MaxSyl: rhyme.MaxSyl,
		Match: rhyme.AlignSuffix, Limit: 10}
	resp, err := snap.Search(e, q)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range resp.Results {
		got = append(got, r.Candidate.Text+"/"+r.Level.String())
	}
	if strings.Join(got, " ") != "honey/perfect" {
		t.Errorf("results = %v (tummies ends in Z, so it is not even a family rhyme of money)", got)
	}
	if d := snap.Dictionary(entry.EN); d == nil || d.Lookup("zyzzyva") == nil {
		t.Error("unpronounceable entries stay in the dictionary")
	}
	tummies := snap.Entry(snap.Dictionary(entry.EN).Lookup("tummies").ID)
	if tummies.Confidence != en.DerivedConfidence || tummies.RhymeError {
		t.Errorf("tummies = %+v", tummies)
	}
}

func TestSlangApprovalAndTrend(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	e := engine(t, "../../configs")
	pipe := ingest.Pipeline{Store: st, Engine: e}
	// A dictionary word to rhyme against.
	if _, err := pipe.Run(ctx, &fakeSource{entries: []entry.Entry{def("dịu", "êm, nhẹ")}}, ingest.Options{}); err != nil {
		t.Fatal(err)
	}
	s := app.NewSlang(st, pipe, nil)
	chill := &slang.Submission{Headword: "chill", Gloss: "thư giãn", Respelling: "chiu", SubmittedBy: "test"}
	flex := &slang.Submission{Headword: "flex", Gloss: "khoe", Respelling: "phờ léc", SubmittedBy: "test"}
	for _, sub := range []*slang.Submission{chill, flex} {
		if err := s.Submit(ctx, sub); err != nil {
			t.Fatal(err)
		}
	}
	approved, err := s.Approve(ctx, "admin", chill.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve(ctx, "admin", flex.ID); err != nil {
		t.Fatal(err)
	}

	var h rhymeindex.Holder
	snap, err := h.Rebuild(ctx, st, e)
	if err != nil {
		t.Fatal(err)
	}
	c := snap.Entry(approved.EntryID)
	if c == nil || !c.IsSlang || c.RhymeError || c.Trend < 0.99 || c.Trend > 1 {
		t.Fatalf("approved slang entry = %+v (the second approval must not mark it stale)", c)
	}
	q := rhyme.NewQuery([]vnphon.Syllable{mustSyl(t, "dịu")})
	q.Level, q.Limit = rhyme.L2, 10
	resp, err := snap.Search(e, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Candidate.Text != "chill" || !resp.Results[0].Candidate.IsSlang {
		t.Errorf("dịu should rhyme with chill (read “chiu”): %+v", resp.Results)
	}

	v, err := s.Bump(ctx, approved.EntryID, 0.2)
	if err != nil || v < 1.19 || v > 1.21 {
		t.Errorf("bump = %v, %v", v, err)
	}
	if _, err := s.Bump(ctx, 999999, 0.2); err == nil {
		t.Error("bumping a missing entry")
	}
	if err := s.Reject(ctx, "admin", flex.ID); err == nil {
		t.Error("an approved submission cannot be rejected")
	}
}

func TestIngesterLock(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	e := engine(t, "../../configs")
	ing := app.NewIngester(st, ingest.Pipeline{Store: st, Engine: e}, map[string]ports.Source{"fake": &fakeSource{entries: lexicon()[:20]}})
	rep, err := ing.Run(ctx, "fake", false)
	if err != nil || rep.Inserted == 0 {
		t.Fatalf("%+v %v", rep, err)
	}
	if last, err := st.LastSuccessfulRun(ctx, "fake"); err != nil || last.IsZero() {
		t.Errorf("last run: %v %v", last, err)
	}
	// While another node holds the lock, Run reports ErrBusy.
	held := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_, _ = st.TryLock(ctx, "ingest:fake", func() error { close(held); <-release; return nil })
	}()
	<-held
	if _, err := ing.Run(ctx, "fake", false); !errors.Is(err, app.ErrBusy) {
		t.Errorf("concurrent run: %v", err)
	}
	close(release)
	if _, err := ing.Run(ctx, "nope", false); !errors.Is(err, app.ErrNotFound) {
		t.Errorf("unknown source: %v", err)
	}
}

func mustSyl(t *testing.T, w string) vnphon.Syllable {
	t.Helper()
	s, err := vnphon.ParseSyllable(w)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
