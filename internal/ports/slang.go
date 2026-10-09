package ports

import (
	"context"

	"playword/internal/domain/entry"
	"playword/internal/domain/slang"
)

// SlangStore keeps submissions and the trend of approved slang. Lookups
// that find nothing return account.ErrNotFound.
type SlangStore interface {
	CreateSubmission(ctx context.Context, s *slang.Submission) error
	SubmissionExists(ctx context.Context, lang entry.Lang, headword, submittedBy string) (bool, error)
	Submissions(ctx context.Context, status string, limit int) ([]slang.Submission, error)
	Submission(ctx context.Context, id int64) (*slang.Submission, error)
	// ReviewSubmission moves a pending submission to approved or rejected;
	// it fails with account.ErrNotFound if it is not pending.
	ReviewSubmission(ctx context.Context, id int64, status string, entryID int64, reviewer string) error
	EntryIDBySource(ctx context.Context, lang entry.Lang, normalized, source string) (int64, error)
	SetTrend(ctx context.Context, entryID int64, score float64) error
	BumpTrend(ctx context.Context, entryID int64, delta float64) (float64, error)
	// NotifyIndex asks index builders to rebuild (after trend changes).
	NotifyIndex(ctx context.Context) error
}
