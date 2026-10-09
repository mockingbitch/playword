package views

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"playword/internal/domain/analytics"
)

// Chart geometry follows the dataviz mark specs: bars at most 24px thick
// with a 4px rounded data end and a square baseline, a hairline baseline,
// sparing labels, values in tooltips and a table view.

const (
	colW      = 640.0
	colH      = 160.0
	colPadTop = 18.0
	colPadBot = 22.0
)

type column struct {
	X, Y, W, H float64
	Path       string
	Title      string
}

// roundedTop is a bar path rounded at the top (data end), square at the base.
func roundedTop(x, y, w, h, r float64) string {
	r = math.Min(r, math.Min(w/2, h))
	return fmt.Sprintf("M%.1f,%.1f V%.1f Q%.1f,%.1f %.1f,%.1f H%.1f Q%.1f,%.1f %.1f,%.1f V%.1f Z",
		x, y+h, y+r, x, y, x+r, y, x+w-r, x+w, y, x+w, y+r, y+h)
}

// roundedRight is a horizontal bar rounded at its right (data) end.
func roundedRight(x, y, w, h, r float64) string {
	r = math.Min(r, math.Min(h/2, w))
	return fmt.Sprintf("M%.1f,%.1f H%.1f Q%.1f,%.1f %.1f,%.1f V%.1f Q%.1f,%.1f %.1f,%.1f H%.1f Z",
		x, y, x+w-r, x+w, y, x+w, y+r, y+h-r, x+w, y+h, x+w-r, y+h, x)
}

// dailyColumns lays out one value per day as columns.
func dailyColumns(days []analytics.Day, value func(analytics.Day) int64, unit string) ([]column, int64) {
	var peak int64
	for _, d := range days {
		peak = max(peak, value(d))
	}
	if len(days) == 0 {
		return nil, 0
	}
	slot := colW / float64(len(days))
	w := math.Min(24, slot*0.7)
	plotH := colH - colPadTop - colPadBot
	out := make([]column, len(days))
	for i, d := range days {
		v := value(d)
		h := 0.0
		if peak > 0 {
			h = math.Max(plotH*float64(v)/float64(peak), 1)
		}
		x := float64(i)*slot + (slot-w)/2
		y := colPadTop + plotH - h
		out[i] = column{X: x, Y: y, W: w, H: h, Path: roundedTop(x, y, w, h, 4),
			Title: d.Day.Format("02/01") + ": " + compact(v) + " " + unit}
	}
	return out, peak
}

type hbar struct {
	Label, Value string
	Path         string
	Y, LabelY    float64
	ValueX       float64
}

const (
	barH     = 16.0
	barGap   = 10.0
	barLabel = 96.0
	barW     = 420.0
)

// hbars lays out one dimension of the breakdown as horizontal bars.
func hbars(rows []analytics.BreakdownRow, dim string) ([]hbar, float64) {
	var peak int64
	var mine []analytics.BreakdownRow
	for _, r := range rows {
		if r.Dim == dim {
			mine = append(mine, r)
			peak = max(peak, r.N)
		}
	}
	out := make([]hbar, len(mine))
	for i, r := range mine {
		y := float64(i) * (barH + barGap)
		w := 0.0
		if peak > 0 {
			w = math.Max(barW*float64(r.N)/float64(peak), 2)
		}
		out[i] = hbar{Label: dimLabel(dim, r.Value), Value: compact(r.N), Path: roundedRight(barLabel, y, w, barH, 4),
			Y: y, LabelY: y + barH*0.75, ValueX: barLabel + w + 6}
	}
	return out, float64(len(mine))*(barH+barGap) + 4
}

func dimLabel(dim, v string) string {
	switch dim {
	case "lang":
		return label(langOptions, v)
	case "dialect":
		return label(dialectOptions, v)
	case "level":
		if v == "perfect" || v == "family" || v == "assonance" {
			return label(enLevelOptions, v)
		}
		return label(levelOptions, v)
	case "n_syllables":
		return v + " âm"
	}
	return v
}

// compact formats counts as 1,284 / 12.9K / 4.2M.
func compact(n int64) string {
	switch {
	case n >= 1_000_000:
		return strconv.FormatFloat(float64(n)/1e6, 'f', 1, 64) + "M"
	case n >= 10_000:
		return strconv.FormatFloat(float64(n)/1e3, 'f', 1, 64) + "K"
	case n >= 1000:
		s := strconv.FormatInt(n, 10)
		return s[:len(s)-3] + "," + s[len(s)-3:]
	}
	return strconv.FormatInt(n, 10)
}

func f1(f float64) string  { return strconv.FormatFloat(f, 'f', 1, 64) }
func pct(f float64) string { return strconv.FormatFloat(f*100, 'f', 1, 64) + "%" }
func fpx(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) }
func ms(f float64) string {
	if f < 1 {
		return "<1 ms"
	}
	return strconv.FormatFloat(f, 'f', 0, 64) + " ms"
}
func day(t time.Time) string { return t.Format("02/01") }

func saveRate(s analytics.SlangStat) string {
	if s.Views == 0 {
		return "—"
	}
	return pct(float64(s.Saves) / float64(s.Views))
}

func rising(r analytics.RisingQuery) string {
	if r.Rising >= 999 {
		return "mới"
	}
	return "×" + f1(r.Rising)
}

func windowLabel(d time.Duration) string {
	return strconv.Itoa(int(d.Hours()/24)) + " ngày"
}
