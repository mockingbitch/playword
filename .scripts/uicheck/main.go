// Command uicheck drives headless Chrome over pages of a running Playword and
// reports what a reviewer would otherwise check by hand: horizontal
// overflow, console errors (CSP violations included), tap targets under
// 44px, and a screenshot per page and width.
//
//	go run ./.scripts/uicheck -base http://localhost:8020 -paths /,/search?q=ngày+tàn -widths 360,768,1440
//
// It is its own module (.scripts/uicheck/go.mod) so chromedp stays out of
// the app's dependencies. Exit status 1 when any page overflows or logs an
// error.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const phoneUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148"

// probe measures the page after load. Elements inside a horizontally
// scrolling container are fine; anything else past the edge is overflow.
const probe = `(() => {
  const W = document.documentElement.clientWidth, over = [], small = [];
  for (const el of document.querySelectorAll('body *')) {
    const r = el.getBoundingClientRect();
    if (!r.width || getComputedStyle(el).position === 'fixed' || el.classList.contains('skip')) continue;
    let p = el.parentElement, inner = false;
    while (p) { const o = getComputedStyle(p).overflowX; if (o === 'auto' || o === 'scroll' || o === 'hidden') { inner = true; break; } p = p.parentElement; }
    if (!inner && (r.right > W + 1 || r.left < -1)) over.push(el.tagName.toLowerCase() + '.' + el.className + ' [' + Math.round(r.left) + ',' + Math.round(r.right) + ']');
    if (el.matches('a,button,input:not([type=hidden]),select,summary,textarea') && (r.height < 44 && r.width < 44)) small.push(el.tagName.toLowerCase() + '.' + el.className);
  }
  return JSON.stringify({width: W, scrollWidth: document.documentElement.scrollWidth, overflow: over.slice(0, 10), smallTargets: [...new Set(small)].slice(0, 10)});
})()`

type report struct {
	Page         string   `json:"page"`
	Width        int64    `json:"width"`
	Theme        string   `json:"theme"`
	ScrollWidth  int      `json:"scrollWidth"`
	Overflow     []string `json:"overflow"`
	SmallTargets []string `json:"smallTargets"`
	Console      []string `json:"console"`
	Screenshot   string   `json:"screenshot"`
}

func main() {
	base := flag.String("base", "http://localhost:8020", "server to check")
	paths := flag.String("paths", "/,/search?q=ng%C3%A0y+t%C3%A0n,/dictionary?q=t%C3%A0n,/write,/about,/login", "comma-separated paths")
	widths := flag.String("widths", "360,390,768,1024,1440", "comma-separated viewport widths; under 768 emulates a phone")
	themes := flag.String("themes", "dark", "dark, light or dark,light")
	out := flag.String("out", ".uicheck-out", "directory for screenshots and report.json")
	full := flag.Bool("full", true, "full-page screenshots")
	chrome := flag.String("chrome", "", "Chrome binary (default: found on PATH)")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fail(err)
	}
	opts := chromedp.DefaultExecAllocatorOptions[:]
	if *chrome != "" {
		opts = append(opts, chromedp.ExecPath(*chrome))
	}
	actx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()
	ctx, cancel := chromedp.NewContext(actx)
	defer cancel()

	var console []string
	chromedp.ListenTarget(ctx, func(ev any) {
		switch e := ev.(type) {
		case *runtime.EventConsoleAPICalled:
			if e.Type == runtime.APITypeError || e.Type == runtime.APITypeWarning {
				for _, a := range e.Args {
					console = append(console, string(e.Type)+": "+strings.Trim(string(a.Value), `"`)+a.Description)
				}
			}
		case *runtime.EventExceptionThrown:
			console = append(console, "exception: "+e.ExceptionDetails.Text)
		}
	})

	var reports []report
	bad := 0
	for _, p := range split(*paths) {
		for _, ws := range split(*widths) {
			w, err := strconv.ParseInt(ws, 10, 64)
			if err != nil {
				fail(fmt.Errorf("width %q: %w", ws, err))
			}
			for _, theme := range split(*themes) {
				console = nil
				r := report{Page: p, Width: w, Theme: theme}
				ua, vp := "", chromedp.EmulateViewport(w, 900)
				if w < 768 {
					ua, vp = phoneUA, chromedp.EmulateViewport(w, 844, chromedp.EmulateMobile)
				}
				var shot []byte
				var res string
				acts := []chromedp.Action{
					vp,
					emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: theme}}),
				}
				if ua != "" {
					acts = append(acts, emulation.SetUserAgentOverride(ua))
				}
				acts = append(acts, chromedp.Navigate(*base+p), chromedp.Sleep(600*time.Millisecond), chromedp.Evaluate(probe, &res))
				if *full {
					acts = append(acts, chromedp.FullScreenshot(&shot, 80))
				} else {
					acts = append(acts, chromedp.CaptureScreenshot(&shot))
				}
				if err := chromedp.Run(ctx, acts...); err != nil {
					r.Console = append(r.Console, "run: "+err.Error())
				}
				var m struct {
					ScrollWidth  int      `json:"scrollWidth"`
					Overflow     []string `json:"overflow"`
					SmallTargets []string `json:"smallTargets"`
				}
				_ = json.Unmarshal([]byte(res), &m)
				r.ScrollWidth, r.Overflow, r.SmallTargets = m.ScrollWidth, m.Overflow, m.SmallTargets
				r.Console = append(r.Console, console...)
				ext := ".jpg"
				if !*full {
					ext = ".png"
				}
				r.Screenshot = filepath.Join(*out, slug(p)+"-"+ws+"-"+theme+ext)
				_ = os.WriteFile(r.Screenshot, shot, 0o644)
				status := "ok"
				if int64(r.ScrollWidth) > w || len(r.Overflow) > 0 || len(r.Console) > 0 {
					status, bad = "FAIL", bad+1
				}
				fmt.Printf("%-4s %-40s %5d %-5s overflow=%d console=%d small=%d\n", status, p, w, theme, len(r.Overflow), len(r.Console), len(r.SmallTargets))
				for _, o := range r.Overflow {
					fmt.Println("       overflow:", o)
				}
				for _, c := range r.Console {
					fmt.Println("       console:", c)
				}
				reports = append(reports, r)
			}
		}
	}
	b, _ := json.MarshalIndent(reports, "", "  ")
	_ = os.WriteFile(filepath.Join(*out, "report.json"), b, 0o644)
	fmt.Printf("%d checks, %d failed; screenshots and report.json in %s\n", len(reports), bad, *out)
	if bad > 0 {
		os.Exit(1)
	}
}

func split(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// slug names a screenshot after its path: /search?q=tàn -> search-q-tan.
func slug(p string) string {
	if u, err := url.PathUnescape(p); err == nil {
		p = u
	}
	var b strings.Builder
	for _, r := range strings.Trim(p, "/") {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			if s := b.String(); s != "" && !strings.HasSuffix(s, "-") {
				b.WriteByte('-')
			}
		}
	}
	if b.Len() == 0 {
		return "home"
	}
	return strings.TrimSuffix(b.String(), "-")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "uicheck:", err)
	os.Exit(2)
}
