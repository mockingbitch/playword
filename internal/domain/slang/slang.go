// Package slang is community-submitted slang awaiting review (spec §2.2).
package slang

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"playword/internal/domain/entry"
)

// Statuses of a submission.
const (
	Pending  = "pending"
	Approved = "approved"
	Rejected = "rejected"
)

// ErrInvalid is matched by every validation failure.
var ErrInvalid = errors.New("invalid slang submission")

// Submission is a proposed slang entry.
type Submission struct {
	ID          int64
	Headword    string
	Lang        entry.Lang
	Gloss       string
	Example     string
	Respelling  string // pronunciation in the language's spelling, for rhyming (Q8)
	Tags        []string
	SubmittedBy string
	Status      string
	EntryID     int64 // set when approved
	CreatedAt   time.Time
	ReviewedAt  time.Time
	ReviewedBy  string
}

// Validate trims and checks a submission before it is stored.
func (s *Submission) Validate() error {
	s.Headword = strings.Join(strings.Fields(s.Headword), " ")
	s.Gloss = strings.TrimSpace(s.Gloss)
	s.Example = strings.TrimSpace(s.Example)
	s.Respelling = strings.Join(strings.Fields(s.Respelling), " ")
	if s.Lang == "" {
		s.Lang = entry.VI
	}
	switch {
	case s.Lang != entry.VI && s.Lang != entry.EN:
		return fmt.Errorf("%w: language must be vi or en", ErrInvalid)
	case s.Headword == "" || utf8.RuneCountInString(s.Headword) > 60:
		return fmt.Errorf("%w: headword must have 1–60 characters", ErrInvalid)
	case len(strings.Fields(s.Headword)) > 5:
		return fmt.Errorf("%w: at most 5 words", ErrInvalid)
	case s.Gloss == "" || utf8.RuneCountInString(s.Gloss) > 500:
		return fmt.Errorf("%w: meaning must have 1–500 characters", ErrInvalid)
	case utf8.RuneCountInString(s.Example) > 500 || utf8.RuneCountInString(s.Respelling) > 60:
		return fmt.Errorf("%w: example or respelling too long", ErrInvalid)
	case len(s.Tags) > 10:
		return fmt.Errorf("%w: at most 10 tags", ErrInvalid)
	}
	for i, t := range s.Tags {
		s.Tags[i] = strings.ToLower(strings.TrimSpace(t))
	}
	return nil
}

// Entry is the dictionary entry an approved submission becomes.
func (s *Submission) Entry() entry.Entry {
	sense := entry.Sense{Gloss: s.Gloss}
	if s.Example != "" {
		sense.Examples = []string{s.Example}
	}
	return entry.Entry{Headword: s.Headword, Lang: s.Lang, Senses: []entry.Sense{sense}, IsSlang: true,
		Tags: append([]string{"slang"}, s.Tags...), Respelling: s.Respelling, SourceRef: fmt.Sprintf("slang:%d", s.ID)}
}
