// Package views holds the templ components of the HTMX interface and the
// small models they render.
package views

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"playword/internal/adapters/index/lookup"
	"playword/internal/adapters/index/rhymeindex"
	"playword/internal/app"
	"playword/internal/domain/account"
	"playword/internal/domain/analytics"
	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme"
	"playword/pkg/vnphon"
)

// Page is what every full page needs: who is signed in and the CSRF token.
type Page struct {
	User  *account.User
	CSRF  string
	Books []account.Book // the user's books, for "save to" (search page only)
	View  string         // ViewCompact or ViewFull: how result lists show
	URL   string         // this page's path and query, for links that come back
	Site  Site           // the deployment: public URL, contact, version
	Desc  string         // meta description; a default when empty
}

// Site is what pages say about the deployment itself.
type Site struct {
	PublicURL string // https://playword.vn, for canonical and Open Graph URLs; may be empty
	Contact   string // contact email shown on the legal pages; may be empty
	Version   string
}

// DefaultDesc describes Playword in search results and link previews.
const DefaultDesc = "Từ điển vần cho rapper Việt: gõ từ cuối câu, nhận về từ có thật, cùng vần, kèm nghĩa. Năm mức vần, ba giọng Bắc, Trung, Nam, có cả tiếng Anh."

func (pc Page) desc() string {
	if pc.Desc != "" {
		return pc.Desc
	}
	return DefaultDesc
}

// abs makes a site path absolute when the public URL is known.
func (pc Page) abs(path string) string { return pc.Site.PublicURL + path }

// canonical is this page without its query, except a search keeps q (and
// lang), so shared results stay shareable.
func (pc Page) canonical() string {
	u, err := url.Parse(pc.URL)
	if err != nil {
		return pc.abs("/")
	}
	q := url.Values{}
	for _, k := range []string{"q", "lang"} {
		if v := u.Query().Get(k); v != "" {
			q.Set(k, v)
		}
	}
	if len(q) > 0 {
		return pc.abs(u.Path + "?" + q.Encode())
	}
	return pc.abs(u.Path)
}

func csrfHeaders(token string) string {
	b, _ := json.Marshal(map[string]string{"X-CSRF-Token": token})
	return string(b)
}

// SearchModel is what the rhyme search page renders.
type SearchModel struct {
	Page    Page
	Params  app.RhymeParams
	Result  *app.RhymeResult // nil before the first query
	Error   string
	Desk    bool // rendered inside the writing desk
	Partial bool // an HTMX swap: also update the screen-reader status
}

// DictModel is what the dictionary page renders.
type DictModel struct {
	Page        Page
	Query       string
	Lang        entry.Lang
	Entry       *rhymeindex.Entry
	Suggestions []lookup.Suggestion
	Meanings    []*rhymeindex.Entry
	Error       string
}

type option struct{ Value, Label, Hint string }

var (
	levelOptions = []option{
		{"L0", "Chuẩn", "Trùng vần và trùng thanh"},
		{"L1", "Cùng bằng/trắc", "Trùng vần, cùng nhóm thanh bằng hoặc trắc"},
		{"L2", "Khác thanh", "Trùng vần, thanh tự do"},
		{"L25", "Rap-flex", "Nguyên âm cùng họ, âm cuối gần nhau"},
		{"L3", "Ép vần", "Chỉ cần nguyên âm cùng họ"},
	}
	enLevelOptions = []option{
		{"perfect", "Perfect", "Same sounds from the matched vowel on"},
		{"family", "Family", "Vowels and consonants from the same groups"},
		{"assonance", "Assonance", "Vowel groups only"},
	}
	langOptions    = []option{{"vi", "Tiếng Việt", ""}, {"en", "English", ""}}
	sylOptions     = []option{{"1", "1+ âm", ""}, {"2", "2+ âm", ""}, {"3", "3+ âm", ""}}
	dialectOptions = []option{{"bac", "Bắc", ""}, {"trung", "Trung", ""}, {"nam", "Nam", ""}}
	matchOptions   = []option{{"suffix", "Cuối cụm", "Vần nằm ở cuối từ/cụm"}, {"prefix", "Đầu cụm", "Vần nằm ở đầu"}, {"any", "Bất kỳ", ""}}
)

func label(opts []option, v string) string {
	for _, o := range opts {
		if o.Value == v {
			return o.Label
		}
	}
	return v
}

// LevelLabel is the Vietnamese name of a level shown in the UI.
func LevelLabel(l rhyme.Level) string {
	if l.IsEnglish() {
		return label(enLevelOptions, l.String())
	}
	return label(levelOptions, l.String())
}

// DialectLabel is the Vietnamese name of a dialect shown in the UI.
func DialectLabel(d rhyme.Dialect) string { return label(dialectOptions, string(d)) }

func isEN(l entry.Lang) bool { return l == entry.EN }

func levelOptionsFor(l entry.Lang) []option {
	if isEN(l) {
		return enLevelOptions
	}
	return levelOptions
}

// langURL switches the search page to another language, keeping the text.
func langURL(path string, text string, l string) string {
	v := url.Values{}
	if text != "" {
		v.Set("q", text)
	}
	if l != string(entry.VI) {
		v.Set("lang", l)
	}
	if len(v) == 0 {
		return path
	}
	return path + "?" + v.Encode()
}

func filterSummary(p app.RhymeParams) string {
	parts := []string{LevelLabel(p.Level), "giọng " + DialectLabel(p.Dialect), label(matchOptions, string(p.Match))}
	if isEN(p.Lang) {
		parts = []string{"English", LevelLabel(p.Level), label(matchOptions, string(p.Match))}
	}
	if p.MinSyl > 1 {
		parts = append(parts, strconv.Itoa(p.MinSyl)+"+ âm")
	}
	return strings.Join(parts, " · ")
}

// SearchURL renders params as a shareable /search URL; mod edits a copy.
func SearchURL(p app.RhymeParams, mod func(*app.RhymeParams)) string {
	if mod != nil {
		mod(&p)
	}
	def := app.DefaultRhymeParams("")
	v := url.Values{}
	v.Set("q", p.Text)
	if p.Lang != def.Lang {
		v.Set("lang", string(p.Lang))
	}
	if p.Dialect != def.Dialect {
		v.Set("dialect", string(p.Dialect))
	}
	if p.Level != def.Level {
		v.Set("level", p.Level.String())
	}
	if p.MinSyl != def.MinSyl {
		v.Set("min_syl", strconv.Itoa(p.MinSyl))
	}
	if p.MaxSyl != def.MaxSyl {
		v.Set("max_syl", strconv.Itoa(p.MaxSyl))
	}
	if p.Match != def.Match {
		v.Set("match", string(p.Match))
	}
	if !p.IncludeSlang {
		v.Set("slang", "false")
	}
	if !p.IncludeProper {
		v.Set("proper", "false")
	}
	if p.ExtraOver != "" {
		v.Set("extra_over", string(p.ExtraOver))
	}
	if p.Cursor != "" {
		v.Set("cursor", p.Cursor)
	}
	return "/search?" + v.Encode()
}

func nextURL(r *app.RhymeResult) string {
	return SearchURL(r.Params, func(p *app.RhymeParams) { p.Cursor = r.NextCursor })
}

func otherDialects(d rhyme.Dialect) []rhyme.Dialect {
	var out []rhyme.Dialect
	for _, x := range rhyme.Dialects {
		if x != d {
			out = append(out, x)
		}
	}
	return out
}

func extraURL(p app.RhymeParams, d rhyme.Dialect) string {
	return SearchURL(p, func(q *app.RhymeParams) { q.ExtraOver, q.Dialect, q.Cursor = q.Dialect, d, "" })
}

// WordURL is the definition page (or HTMX partial) of a headword.
func WordURL(lang entry.Lang, text string) string {
	return "/word/" + url.PathEscape(string(lang)) + "/" + url.PathEscape(text)
}

func rhymeURL(text string) string { return "/?q=" + url.QueryEscape(text) }
func dictURL(text string) string  { return "/dictionary?q=" + url.QueryEscape(text) }

func rhymeURLLang(l entry.Lang, text string) string {
	if isEN(l) {
		return rhymeURL(text) + "&lang=en"
	}
	return rhymeURL(text)
}

func dictURLLang(l entry.Lang, text string) string {
	if isEN(l) {
		return dictURL(text) + "&lang=en"
	}
	return dictURL(text)
}
func words(text string) []string { return vnphon.SplitWords(text) }
func itoa(n int) string          { return strconv.Itoa(n) }
func i64(n int64) string         { return strconv.FormatInt(n, 10) }

// inWindow reports whether word i of a hit's headword is part of the match.
// Vietnamese windows count syllables, which are words; English windows
// count rhyme segments, mapped back to words through SegWord.
func inWindow(i int, h app.Hit) bool {
	if h.Entry.Lang != entry.EN {
		return i >= h.Start && i < h.End
	}
	if h.Entry.SegWord == nil {
		return true
	}
	for j := h.Start; j < h.End && j < len(h.Entry.SegWord); j++ {
		if h.Entry.SegWord[j] == i {
			return true
		}
	}
	return false
}

func isProper(e *rhymeindex.Entry) bool {
	for _, t := range e.Tags {
		if t == "proper_noun" {
			return true
		}
	}
	return false
}

func firstSense(e *rhymeindex.Entry) entry.Sense {
	if len(e.Senses) == 0 {
		return entry.Sense{}
	}
	return e.Senses[0]
}

// matchedPart is the query's matched tail, e.g. "tàn" or "ngày tàn"; for
// English the phonemes of the matched segments, e.g. "AH1 N IY0".
func matchedPart(r *app.RhymeResult, n int) string {
	if r.Params.Lang == entry.EN {
		segs := r.QuerySegs[max(0, len(r.QuerySegs)-n):]
		parts := make([]string, len(segs))
		for i, s := range segs {
			parts[i] = s.String()
		}
		return strings.Join(parts, " ")
	}
	if n > len(r.Query) {
		n = len(r.Query)
	}
	s, _ := vnphon.RenderPhrase(r.Query[len(r.Query)-n:])
	return s
}

func splitHL(word string, n int) (string, string) {
	r := []rune(word)
	n = min(max(n, 0), len(r))
	return string(r[:n]), string(r[n:])
}

func showGroup(hits []app.Hit, i, prev int) bool {
	if i == 0 {
		return hits[0].Matched != prev
	}
	return hits[i].Matched != hits[i-1].Matched
}

func pageTitle(text string) string {
	if text == "" {
		return "Playword — từ điển vần"
	}
	return "Vần cho “" + text + "” — Playword"
}

func dictTitle(q string) string {
	if q == "" {
		return "Từ điển — Playword"
	}
	return q + " — Từ điển Playword"
}

func moreMark(r *app.RhymeResult) string {
	if r.NextCursor != "" {
		return "+"
	}
	return ""
}

// WordURLFor is WordURL for a stored language code.
func WordURLFor(lang, text string) string {
	if lang == "" {
		lang = string(entry.VI)
	}
	return WordURL(entry.Lang(lang), text)
}

func levelNote(level string) string {
	if level == "" {
		return ""
	}
	if l, err := rhyme.ParseLevel(level); err == nil {
		return " · " + LevelLabel(l)
	}
	return ""
}

func langOf(s string) entry.Lang {
	if s == string(entry.EN) {
		return entry.EN
	}
	return entry.VI
}

func lastSeen(u analytics.UserRow) string {
	if u.LastSeen.IsZero() {
		return "—"
	}
	return u.LastSeen.Format("02/01/2006")
}

type csrfCtxKey struct{}

// WithCSRF lets components deep in a page (a book item) embed the CSRF
// token without threading it through every call.
func WithCSRF(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, csrfCtxKey{}, token)
}

func csrfOf(ctx context.Context) string {
	t, _ := ctx.Value(csrfCtxKey{}).(string)
	return t
}
