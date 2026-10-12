// Package analytics is usage events and the dashboard's aggregates (M7).
package analytics

import "time"

// Event types (spec addendum §3).
const (
	Search      = "search"
	ViewWord    = "view_word"
	ClickResult = "click_result"
	SaveItem    = "save_item"
	SubmitSlang = "submit_slang"
	Login       = "login"
	RhymeCheck  = "rhyme_check" // props: the rhyme scheme and counts, never the lyrics
)

// Event is one append-only usage record. It carries no raw PII: a user id
// or a random anonymous cookie, never an IP address.
type Event struct {
	TS          time.Time
	UserID      string // empty for guests
	AnonID      string // random cookie of a guest
	Type        string
	Lang        string
	Dialect     string
	Level       string
	Query       string
	NSyllables  int
	EntryID     int64
	ResultCount int // -1 when not applicable
	LatencyMS   int // -1 when not measured
	Props       map[string]any
}

// Summary is the dashboard's headline numbers for a period.
type Summary struct {
	DAU, WAU, MAU   int64
	Searches, Saves int64
	NewUsers        int64
	P50Ms, P99Ms    float64
	Since, Until    time.Time
	EventsDropped   uint64 // by this server since it started
	EventsRecorded  uint64
}

// Day is one row of daily activity.
type Day struct {
	Day                      time.Time
	Actives, Searches, Saves int64
}

// QueryCount is a query and how often it was searched.
type QueryCount struct {
	Lang, Query string
	N           int64
}

// RisingQuery compares the last 7 days with the 30 before (per week).
type RisingQuery struct {
	Lang, Query string
	C7, C30     int64
	Rising      float64 // 999 when the query is new
}

// BreakdownRow counts searches by one dimension.
type BreakdownRow struct {
	Dim, Value string
	N          int64
}

// SlangStat is how a slang entry is used.
type SlangStat struct {
	EntryID      int64
	Headword     string
	Trend        float64
	Views, Saves int64
}

// UserRow is one line of the admin user table.
type UserRow struct {
	ID, Email, DisplayName, Role string
	CreatedAt, LastSeen          time.Time
	Searches, Books, Items       int64
}
