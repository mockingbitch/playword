package postgres

import (
	"context"

	"playword/internal/adapters/postgres/db"
	"playword/internal/domain/entry"
	"playword/internal/domain/slang"
	"playword/internal/ports"
)

var _ ports.SlangStore = (*Store)(nil)

func (s *Store) CreateSubmission(ctx context.Context, sub *slang.Submission) error {
	tags := sub.Tags
	if tags == nil {
		tags = []string{}
	}
	r, err := db.New(s.pool).CreateSubmission(ctx, db.CreateSubmissionParams{Headword: sub.Headword, Lang: string(sub.Lang),
		Gloss: sub.Gloss, Example: sub.Example, Respelling: sub.Respelling, Tags: tags, SubmittedBy: sub.SubmittedBy})
	if err != nil {
		return err
	}
	sub.ID, sub.CreatedAt, sub.Status = r.ID, ts(r.CreatedAt), slang.Pending
	return nil
}

func (s *Store) Submissions(ctx context.Context, status string, limit int) ([]slang.Submission, error) {
	rows, err := db.New(s.pool).ListSubmissions(ctx, db.ListSubmissionsParams{Status: status, Limit: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]slang.Submission, len(rows))
	for i, r := range rows {
		out[i] = slang.Submission{ID: r.ID, Headword: r.Headword, Lang: entry.Lang(r.Lang), Gloss: r.Gloss, Example: r.Example,
			Respelling: r.Respelling, Tags: r.Tags, SubmittedBy: r.SubmittedBy, Status: r.Status, EntryID: r.EntryID,
			CreatedAt: ts(r.CreatedAt), ReviewedAt: ts(r.ReviewedAt), ReviewedBy: r.ReviewedBy}
	}
	return out, nil
}

func (s *Store) Submission(ctx context.Context, id int64) (*slang.Submission, error) {
	r, err := db.New(s.pool).GetSubmission(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	return &slang.Submission{ID: r.ID, Headword: r.Headword, Lang: entry.Lang(r.Lang), Gloss: r.Gloss, Example: r.Example,
		Respelling: r.Respelling, Tags: r.Tags, SubmittedBy: r.SubmittedBy, Status: r.Status, EntryID: r.EntryID,
		CreatedAt: ts(r.CreatedAt), ReviewedAt: ts(r.ReviewedAt), ReviewedBy: r.ReviewedBy}, nil
}

func (s *Store) ReviewSubmission(ctx context.Context, id int64, status string, entryID int64, reviewer string) error {
	return affected(db.New(s.pool).ReviewSubmission(ctx, db.ReviewSubmissionParams{ID: id, Status: status,
		EntryID: entryID, ReviewedBy: nullable(reviewer)}))
}

func (s *Store) EntryIDBySource(ctx context.Context, lang entry.Lang, normalized, source string) (int64, error) {
	id, err := db.New(s.pool).EntryIDBySource(ctx, db.EntryIDBySourceParams{Lang: string(lang), Normalized: normalized, Name: source})
	return id, notFound(err)
}

func (s *Store) SetTrend(ctx context.Context, entryID int64, score float64) error {
	return db.New(s.pool).SetTrend(ctx, db.SetTrendParams{ID: entryID, TrendScore: score})
}

func (s *Store) BumpTrend(ctx context.Context, entryID int64, delta float64) (float64, error) {
	v, err := db.New(s.pool).BumpTrend(ctx, db.BumpTrendParams{ID: entryID, Delta: delta})
	return v, notFound(err)
}

func (s *Store) NotifyIndex(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, "select pg_notify($1, 'trend')", NotifyChannel)
	return err
}

func (s *Store) SubmissionExists(ctx context.Context, lang entry.Lang, headword, submittedBy string) (bool, error) {
	return db.New(s.pool).SubmissionExists(ctx, db.SubmissionExistsParams{Lang: string(lang), Headword: headword,
		SubmittedBy: nullable(submittedBy)})
}
