package httpadapter

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"playword/internal/app"
	"playword/internal/domain/account"
	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme"
)

// rhymeParamsFor is rhymeParams with a signed-in user's saved defaults for
// the language, dialect and level they did not pass.
func rhymeParamsFor(v url.Values, u *account.User) (app.RhymeParams, error) {
	if u != nil {
		v = cloneValues(v)
		if v.Get("lang") == "" && u.DefaultLang != "" && u.DefaultLang != string(entry.VI) {
			v.Set("lang", u.DefaultLang)
		}
		if v.Get("dialect") == "" && u.DefaultDialect != "" {
			v.Set("dialect", u.DefaultDialect)
		}
		if v.Get("level") == "" && u.DefaultLevel != "" {
			if l, err := rhyme.ParseLevel(u.DefaultLevel); err == nil && l.IsEnglish() == (v.Get("lang") == string(entry.EN)) {
				v.Set("level", u.DefaultLevel)
			}
		}
	}
	return rhymeParams(v)
}

func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v))
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// rhymeParams reads spec §6 query parameters; absent ones keep defaults.
// For repeated parameters the last value wins, so a form can send a hidden
// "false" before a checkbox's "true".
func rhymeParams(v url.Values) (app.RhymeParams, error) {
	last := func(k string) string {
		if vs := v[k]; len(vs) > 0 {
			return strings.TrimSpace(vs[len(vs)-1])
		}
		return ""
	}
	p := app.DefaultRhymeParams(last("q"))
	var err error
	if s := last("lang"); s != "" {
		p.Lang = entry.Lang(s)
	}
	if s := last("dialect"); s != "" {
		if p.Dialect, err = rhyme.ParseDialect(s); err != nil {
			return p, err
		}
	}
	if s := last("extra_over"); s != "" {
		if p.ExtraOver, err = rhyme.ParseDialect(s); err != nil {
			return p, err
		}
	}
	if s := last("level"); s != "" {
		if p.Level, err = rhyme.ParseLevel(s); err != nil {
			return p, err
		}
	} else if p.Lang == entry.EN {
		p.Level = rhyme.Family // the English counterpart of rap-flex
	}
	if p.Lang == entry.EN && !p.Level.IsEnglish() {
		return p, fmt.Errorf("%w: level %s is not an English level (perfect, family, assonance)", rhyme.ErrInvalidQuery, p.Level)
	}
	if p.Lang != entry.EN && p.Level.IsEnglish() {
		return p, fmt.Errorf("%w: level %s is for English", rhyme.ErrInvalidQuery, p.Level)
	}
	if s := last("match"); s != "" {
		if p.Match, err = rhyme.ParseAlign(s); err != nil {
			return p, err
		}
	}
	for _, f := range []struct {
		name string
		dst  *int
	}{{"min_syl", &p.MinSyl}, {"max_syl", &p.MaxSyl}, {"limit", &p.Limit}} {
		if s := last(f.name); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil {
				return p, fmt.Errorf("%w: %s=%q", rhyme.ErrInvalidQuery, f.name, s)
			}
			*f.dst = n
		}
	}
	for _, f := range []struct {
		name string
		dst  *bool
	}{{"slang", &p.IncludeSlang}, {"proper", &p.IncludeProper}} {
		if s := last(f.name); s != "" {
			b, err := strconv.ParseBool(s)
			if err != nil {
				return p, fmt.Errorf("%w: %s=%q", rhyme.ErrInvalidQuery, f.name, s)
			}
			*f.dst = b
		}
	}
	if p.MinSyl > p.MaxSyl {
		p.MaxSyl = rhyme.MaxSyl
	}
	p.Cursor = last("cursor")
	return p, nil
}

func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }
