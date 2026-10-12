package httpadapter

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"sync"

	"playword/internal/adapters/http/views"
	"playword/internal/app"
	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme"
)

func init() {
	// Not in Go's built-in table, and distroless images have no mime.types.
	_ = mime.AddExtensionType(".woff2", "font/woff2")
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// The words the site's examples rhyme with. Every example shown is a real
// search result for them, so the pages never show an invented word.
const (
	demoText  = "ngày tàn"
	levelWord = "tàn"
	enWord    = "money"
)

// siteCache keeps the examples for one index generation: they only change
// when the index does.
type siteCache struct {
	mu   sync.Mutex
	gen  uint64
	data *views.SiteData
}

func (s *Server) siteData(ctx context.Context) views.SiteData {
	snap, err := s.svc.Snapshot()
	if err != nil {
		return views.SiteData{}
	}
	s.site.mu.Lock()
	defer s.site.mu.Unlock()
	if s.site.data != nil && s.site.gen == snap.Gen {
		return *s.site.data
	}
	d := views.SiteData{Entries: snap.Len(), DemoText: demoText, Word: levelWord, ENWord: enWord, Sources: snap.SourceCounts()}
	demo := app.DefaultRhymeParams(demoText)
	demo.Limit = 14
	if r, err := s.svc.SearchRhymes(ctx, demo); err == nil {
		kept := r.Hits[:0:0]
		for _, h := range r.Hits {
			if showcase(h) {
				kept = append(kept, h)
			}
		}
		r.Hits = kept
		d.Demo = r
	}
	d.Levels = s.levelExamples(ctx, entry.VI, levelWord, rhyme.L3)
	d.EN = s.levelExamples(ctx, entry.EN, enWord, rhyme.Assonance)
	for _, dl := range []rhyme.Dialect{rhyme.Nam, rhyme.Trung} {
		p := app.DefaultRhymeParams(levelWord)
		p.Dialect, p.ExtraOver, p.Level, p.Limit = dl, rhyme.Bac, rhyme.L0, 8
		if r, err := s.svc.SearchRhymes(ctx, p); err == nil {
			d.Dialects = append(d.Dialects, views.DialectExample{Dialect: dl, Hits: r.Hits})
		}
	}
	if ctx.Err() == nil { // a cancelled request must not cache a partial page
		s.site.gen, s.site.data = snap.Gen, &d
	}
	return d
}

// levelExamples finds, for each level from the strictest to loosest, a few
// words that match word at exactly that level. One search per level: in a
// single loose search the strict matches crowd out the loose ones.
func (s *Server) levelExamples(ctx context.Context, lang entry.Lang, word string, loosest rhyme.Level) []views.LevelExample {
	levels := []rhyme.Level{rhyme.L0, rhyme.L1, rhyme.L2, rhyme.L25, rhyme.L3}
	if lang == entry.EN {
		levels = []rhyme.Level{rhyme.Perfect, rhyme.Family, rhyme.Assonance}
	}
	var out []views.LevelExample
	for _, l := range levels {
		p := app.DefaultRhymeParams(word)
		p.Lang, p.Level, p.Limit, p.IncludeProper, p.IncludeSlang = lang, l, app.MaxLimit, false, false
		if lang == entry.VI {
			p.MaxSyl = 1 // one-syllable words show the levels most plainly
		}
		ex := views.LevelExample{Level: l}
		for {
			r, err := s.svc.SearchRhymes(ctx, p)
			if err != nil {
				return out
			}
			for _, h := range r.Hits {
				if h.Level == l && !strings.EqualFold(h.Entry.Headword, word) && showcase(h) && len(ex.Hits) < 8 {
					ex.Hits = append(ex.Hits, h)
				}
			}
			if len(ex.Hits) >= 8 || r.NextCursor == "" {
				break
			}
			p.Cursor = r.NextCursor
		}
		if len(ex.Hits) > 0 {
			out = append(out, ex)
		}
		if l == loosest {
			break
		}
	}
	return out
}

func (s *Server) sitePage(desc string) func(r *http.Request) views.SiteModel {
	return func(r *http.Request) views.SiteModel {
		pc := s.page(r)
		pc.Desc = desc
		return views.SiteModel{Page: pc}
	}
}

// pageHome is the landing page; with ?q= it is the search page, so old
// links (/?q=…) keep working.
func (s *Server) pageHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Has("q") {
		s.pageSearch(w, r)
		return
	}
	m := s.sitePage("")(r)
	m.Data = s.siteData(r.Context())
	render(w, r, http.StatusOK, views.HomePage(m))
}

func (s *Server) pageAbout(w http.ResponseWriter, r *http.Request) {
	m := s.sitePage("Playword là gì, cách tìm vần, năm mức vần và ba giọng đọc Bắc, Trung, Nam, kèm ví dụ thật.")(r)
	m.Data = s.siteData(r.Context())
	render(w, r, http.StatusOK, views.AboutPage(m))
}

func (s *Server) pageSources(w http.ResponseWriter, r *http.Request) {
	m := s.sitePage("Các từ điển mở, kho tần suất và phần mềm Playword dùng, cùng giấy phép của từng nguồn.")(r)
	m.Data = s.siteData(r.Context())
	render(w, r, http.StatusOK, views.SourcesPage(m))
}

func (s *Server) pageTerms(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, views.TermsPage(s.sitePage("Điều khoản sử dụng Playword.")(r)))
}

func (s *Server) pagePrivacy(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, views.PrivacyPage(s.sitePage("Playword lưu những gì, trong bao lâu, và quyền của bạn với dữ liệu của mình.")(r)))
}

// pageNotFound answers every GET no route matches: a page for people, a
// problem document for the API.
func (s *Server) pageNotFound(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeProblemStatus(w, r, http.StatusNotFound, "not-found", "Không tìm thấy", r.URL.Path)
		return
	}
	render(w, r, http.StatusNotFound, views.NotFoundPage(s.page(r)))
}

func (s *Server) robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprint(w, "User-agent: *\nDisallow: /api/\nDisallow: /admin\nDisallow: /me\nDisallow: /books\nDisallow: /save\nDisallow: /view\n")
	if s.opts.PublicURL != "" {
		fmt.Fprintf(w, "Sitemap: %s/sitemap.xml\n", s.opts.PublicURL)
	}
}

// sitemap lists the pages worth indexing (it needs the public URL).
func (s *Server) sitemap(w http.ResponseWriter, r *http.Request) {
	if s.opts.PublicURL == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"+`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`+"\n")
	for _, p := range []string{"/", "/search", "/dictionary", "/write", "/about", "/sources", "/terms", "/privacy"} {
		fmt.Fprintf(w, "  <url><loc>%s%s</loc></url>\n", s.opts.PublicURL, p)
	}
	fmt.Fprint(w, "</urlset>\n")
}

// coarse marks words a dictionary flags as vulgar or offensive.
var coarse = []string{"thô tục", "tục tĩu", "chửi", "giao hợp", "vulgar", "obscene", "offensive", "derogatory", "sexual intercourse", "slur"}

// showcase keeps the site's example words fit for a front page. It only
// picks examples; search results are never filtered this way.
func showcase(h app.Hit) bool {
	for _, s := range h.Entry.Senses {
		g := strings.ToLower(s.Gloss)
		for _, c := range coarse {
			if strings.Contains(g, c) {
				return false
			}
		}
	}
	return true
}
