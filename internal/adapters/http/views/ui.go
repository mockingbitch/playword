package views

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"playword/internal/adapters/index/rhymeindex"
	"playword/internal/app"
	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme"
	"playword/pkg/vnphon"
)

// View modes of the result list: compact is a grid of words (meaning one tap
// away), full is a list with the first meaning of each word.
const (
	ViewCompact = "compact"
	ViewFull    = "full"
)

// strictness orders levels from the strictest (0) to the loosest (4), in
// both languages, so one ramp of styles encodes them.
func strictness(l rhyme.Level) int {
	switch l {
	case rhyme.L0, rhyme.Perfect:
		return 0
	case rhyme.L1:
		return 1
	case rhyme.L2, rhyme.Family:
		return 2
	case rhyme.L25:
		return 3
	}
	return 4
}

func levelClass(l rhyme.Level) string { return "s" + strconv.Itoa(strictness(l)) }

// legend lists the levels a result list can contain: the selected one and
// every stricter one.
func legend(p app.RhymeParams) []option {
	var out []option
	for _, o := range levelOptionsFor(p.Lang) {
		if l, err := rhyme.ParseLevel(o.Value); err == nil && strictness(l) <= strictness(p.Level) {
			out = append(out, o)
		}
	}
	return out
}

func legendClass(value string) string {
	l, _ := rhyme.ParseLevel(value)
	return "sw " + levelClass(l)
}

// wordPart is one word of a hit's headword, split where its rime begins.
type wordPart struct {
	Head, Rime string // Rime is empty for words outside the matched window
}

// hitWords splits a hit's headword for display: in the matched window only
// the rime is highlighted ("ng" + "ày"), so the rapper sees what rhymes.
func hitWords(h app.Hit) []wordPart {
	ws := words(h.Entry.Headword)
	out := make([]wordPart, len(ws))
	for i, w := range ws {
		if !inWindow(i, h) {
			out[i] = wordPart{Head: w}
			continue
		}
		n := 0
		if h.Entry.Lang != entry.EN {
			n = vnphon.OnsetLen(w)
		}
		r := []rune(w)
		out[i] = wordPart{Head: string(r[:n]), Rime: string(r[n:])}
	}
	return out
}

var toneNames = [...]string{vnphon.ToneNgang: "ngang", vnphon.ToneHuyen: "huyền", vnphon.ToneSac: "sắc",
	vnphon.ToneHoi: "hỏi", vnphon.ToneNga: "ngã", vnphon.ToneNang: "nặng"}

// rimePart is the rime of one syllable of a headword, for the word header.
type rimePart struct{ Rime, Tone string }

// rimeInfo is what each syllable of a Vietnamese headword rhymes on.
func rimeInfo(e *rhymeindex.Entry) []rimePart {
	if e.Lang == entry.EN || len(e.Syllables) == 0 {
		return nil
	}
	ws := words(e.Headword)
	if len(ws) != len(e.Syllables) {
		return nil
	}
	out := make([]rimePart, len(ws))
	for i, w := range ws {
		r, ok := vnphon.WrittenRime(w)
		if !ok {
			return nil
		}
		out[i] = rimePart{Rime: r, Tone: toneNames[e.Syllables[i].Tone]}
	}
	return out
}

// countLabel is the size of a result list: the engine ranks at most
// app.MaxDepth results.
func countLabel(r *app.RhymeResult) string {
	if r.Total >= app.MaxDepth {
		return strconv.Itoa(r.Total) + "+ vần"
	}
	return strconv.Itoa(r.Total) + " vần"
}

// statusText is announced to screen readers after each search.
func statusText(m SearchModel) string {
	switch {
	case m.Error != "":
		return m.Error
	case m.Result == nil:
		return ""
	case len(m.Result.Hits) == 0:
		return "Không có vần cho " + m.Params.Text
	}
	return countLabel(m.Result) + " cho " + m.Params.Text
}

// groupLabel heads a run of hits matching the same number of syllables.
func groupLabel(r *app.RhymeResult, h app.Hit) string {
	if r.Params.Lang == entry.EN {
		return "Khớp " + itoa(h.Matched) + " âm"
	}
	return "Khớp " + itoa(h.Matched) + " âm · “" + matchedPart(r, h.Matched) + "”"
}

// groupTitle shows the matched sounds on hover (ARPAbet for English).
func groupTitle(r *app.RhymeResult, h app.Hit) string {
	if r.Params.Lang == entry.EN {
		return matchedPart(r, h.Matched)
	}
	return ""
}

// showProper marks where proper nouns start inside a group (they rank last).
func showProper(r *app.RhymeResult, i int) bool {
	h := r.Hits[i]
	if !isProper(h.Entry) {
		return false
	}
	var prev *app.Hit
	if i > 0 {
		prev = &r.Hits[i-1]
	} else {
		prev = r.Prev
	}
	return prev == nil || prev.Matched != h.Matched || !isProper(prev.Entry)
}

func showGroupHit(r *app.RhymeResult, i int) bool { return showGroup(r.Hits, i, r.PrevMatched) }

// panelURL opens a hit's meaning in the side panel (bottom sheet on phones),
// carrying what the save button and the "insert" button need.
func panelURL(h app.Hit, p app.RhymeParams, desk bool) string {
	v := url.Values{}
	v.Set("from", "rhyme")
	v.Set("q", p.Text)
	v.Set("dialect", string(p.Dialect))
	v.Set("level", p.Level.String())
	// the other filters too, so the panel explains the same ranking (only
	// when they differ from the defaults, to keep the link short)
	d := app.DefaultRhymeParams(p.Text)
	if p.MinSyl != d.MinSyl {
		v.Set("min_syl", strconv.Itoa(p.MinSyl))
	}
	if p.MaxSyl != d.MaxSyl {
		v.Set("max_syl", strconv.Itoa(p.MaxSyl))
	}
	if p.Match != d.Match {
		v.Set("match", string(p.Match))
	}
	if !p.IncludeSlang {
		v.Set("slang", "false")
	}
	if !p.IncludeProper {
		v.Set("proper", "false")
	}
	v.Set("hl", h.Level.String())
	if desk {
		v.Set("desk", "1")
	}
	return WordURL(p.Lang, h.Entry.Headword) + "?" + v.Encode()
}

// looser is the loosest level of a language, offered when nothing matched.
func looser(p app.RhymeParams) (app.RhymeParams, bool) {
	l := rhyme.L3
	if isEN(p.Lang) {
		l = rhyme.Assonance
	}
	if p.Level == l {
		return p, false
	}
	p.Level, p.Cursor, p.ExtraOver = l, "", ""
	return p, true
}

// latinOnly reports text written without Vietnamese letters, which may be
// English typed on the Vietnamese tab.
func latinOnly(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

// sensesShown is how many meanings show before "xem thêm".
const sensesShown = 3

func headSenses(e *rhymeindex.Entry) []entry.Sense {
	return e.Senses[:min(len(e.Senses), sensesShown)]
}

func moreSenses(e *rhymeindex.Entry) []entry.Sense {
	if len(e.Senses) <= sensesShown {
		return nil
	}
	return e.Senses[sensesShown:]
}

// viewURL switches the result view without JavaScript (the script flips it
// in place and sets the same cookie).
func viewURL(mode, next string) string {
	return "/view?mode=" + mode + "&next=" + url.QueryEscape(next)
}

func otherView(mode string) string {
	if mode == ViewCompact {
		return ViewFull
	}
	return ViewCompact
}

func viewLabel(mode string) string {
	if mode == ViewCompact {
		return "Gọn"
	}
	return "Đủ"
}

func initial(name string) string {
	for _, r := range strings.ToUpper(name) {
		return string(r)
	}
	return "?"
}

func year() int { return time.Now().Year() }

func versionNote(v string) string {
	if v == "" || v == "dev" {
		return ""
	}
	return " · " + v
}

// tryURL is an example query on the home page; Latin-only words are English.
func tryURL(w string) string {
	if latinOnly(w) {
		return "/search?lang=en&q=" + url.QueryEscape(w)
	}
	return "/search?q=" + url.QueryEscape(w)
}

func levelHint(l rhyme.Level) string {
	for _, o := range levelOptionsFor(langOfLevel(l)) {
		if o.Value == l.String() {
			return o.Hint
		}
	}
	return ""
}

func langOfLevel(l rhyme.Level) entry.Lang {
	if l.IsEnglish() {
		return entry.EN
	}
	return entry.VI
}

// thousands writes 169593 as 169.593, the Vietnamese way.
func thousands(n int) string {
	s := strconv.Itoa(n)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func countOrDash(n int) string {
	if n == 0 {
		return "—"
	}
	return thousands(n)
}

// whyLine is one matched syllable explained for the word panel: "ngày ~
// dày: ay = ay".
type whyLine struct {
	Query, Word  string // the two syllables as written
	QRime, WRime string // their rimes without tone marks
	Same         bool   // the rimes sound the same (in the chosen dialect)
	Notes        []string
}

// whyLines turns the engine's explanation into words. The rules are the
// engine's (rhyme.Diff); this only names them.
func whyLines(x *app.Explanation, d rhyme.Dialect) []whyLine {
	if x == nil {
		return nil
	}
	out := make([]whyLine, 0, len(x.Pairs))
	for _, p := range x.Pairs {
		q, err1 := vnphon.Render(p.Query)
		w, err2 := vnphon.Render(p.Candidate)
		if err1 != nil || err2 != nil {
			return nil
		}
		qr, _ := vnphon.WrittenRime(q)
		wr, _ := vnphon.WrittenRime(w)
		l := whyLine{Query: q, Word: w, QRime: qr, WRime: wr,
			Same: p.Diff&^(rhyme.DiffTone|rhyme.DiffToneGroup) == 0}
		switch {
		case p.Identical:
			l.Notes = append(l.Notes, "trùng từ đã tra")
		case p.Diff == 0 && (qr != wr || p.Query.Tone != p.Candidate.Tone):
			l.Notes = append(l.Notes, "đọc giống nhau ở giọng "+DialectLabel(d))
		case p.Diff == 0 && p.Query.Tone == p.Candidate.Tone:
			l.Notes = append(l.Notes, "trùng vần, trùng thanh")
		}
		if p.Diff&rhyme.DiffMedial != 0 {
			l.Notes = append(l.Notes, "lệch âm đệm")
		}
		if p.Diff&rhyme.DiffNucleus != 0 {
			l.Notes = append(l.Notes, "nguyên âm gần")
		}
		switch {
		case p.Diff&rhyme.DiffCoda != 0:
			l.Notes = append(l.Notes, "âm cuối gần")
		case p.Diff&rhyme.DiffCodaFamily != 0:
			l.Notes = append(l.Notes, "âm cuối cùng họ")
		}
		tones := toneNames[p.Query.Tone] + " / " + toneNames[p.Candidate.Tone]
		switch {
		case p.Diff&rhyme.DiffTone != 0:
			l.Notes = append(l.Notes, "khác thanh, cùng "+toneGroup(p.Query.Tone)+" ("+tones+")")
		case p.Diff&rhyme.DiffToneGroup != 0:
			l.Notes = append(l.Notes, "khác thanh, bằng – trắc ("+tones+")")
		}
		out = append(out, l)
	}
	return out
}

func toneGroup(t int) string {
	if t <= vnphon.ToneHuyen {
		return "bằng"
	}
	return "trắc"
}
