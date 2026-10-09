package postgres

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"

	"playword/internal/adapters/postgres/db"
	"playword/internal/domain/entry"
)

// FreqSource describes a frequency data set (ADR-003 D2).
type FreqSource struct {
	Name, License, URL string
	Lang               entry.Lang
}

// FreqRow is the Zipf frequency of one phrase.
type FreqRow struct {
	Normalized string
	Zipf       float64
}

// Vocab lists every headword of lang, the phrases a frequency count needs.
func (s *Store) Vocab(ctx context.Context, lang entry.Lang) ([]string, error) {
	return db.New(s.pool).ListVocab(ctx, string(lang))
}

// ReplaceWordFreq swaps all rows of one frequency source in a transaction
// and records the run; a dry run rolls back.
func (s *Store) ReplaceWordFreq(ctx context.Context, src FreqSource, rows []FreqRow, stats map[string]any, dryRun bool) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)

	srcID, err := q.UpsertSource(ctx, db.UpsertSourceParams{Name: src.Name, Kind: "frequency", Lang: string(src.Lang),
		License: src.License, Url: pgtype.Text{String: src.URL, Valid: src.URL != ""}})
	if err != nil {
		return 0, err
	}
	runID, err := q.CreateRun(ctx, db.CreateRunParams{SourceID: srcID, FullRun: true})
	if err != nil {
		return 0, err
	}
	if err := q.DeleteWordFreq(ctx, srcID); err != nil {
		return 0, err
	}
	params := make([]db.InsertWordFreqParams, len(rows))
	for i, r := range rows {
		params[i] = db.InsertWordFreqParams{Lang: string(src.Lang), Normalized: r.Normalized, Zipf: r.Zipf, SourceID: srcID}
	}
	if _, err := q.InsertWordFreq(ctx, params); err != nil {
		return 0, err
	}
	statsJSON, _ := json.Marshal(stats)
	if err := q.FinishRun(ctx, db.FinishRunParams{ID: runID, Status: "ok", Inserted: int32(len(rows)), Errors: statsJSON}); err != nil {
		return 0, err
	}
	if dryRun {
		return runID, nil
	}
	if _, err := tx.Exec(ctx, "select pg_notify($1, $2)", NotifyChannel, strconv.FormatInt(runID, 10)); err != nil {
		return 0, err
	}
	return runID, tx.Commit(ctx)
}

// PronRow is one pronunciation (ADR-003 D2).
type PronRow struct {
	Normalized string
	Phonemes   []string
	Confidence float32
}

// ReplacePronunciations swaps all rows of one pronunciation source; a dry
// run rolls back.
func (s *Store) ReplacePronunciations(ctx context.Context, src FreqSource, rows []PronRow, dryRun bool) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	srcID, err := q.UpsertSource(ctx, db.UpsertSourceParams{Name: src.Name, Kind: "pronunciation", Lang: string(src.Lang),
		License: src.License, Url: pgtype.Text{String: src.URL, Valid: src.URL != ""}})
	if err != nil {
		return 0, err
	}
	runID, err := q.CreateRun(ctx, db.CreateRunParams{SourceID: srcID, FullRun: true})
	if err != nil {
		return 0, err
	}
	if err := q.DeletePronunciations(ctx, srcID); err != nil {
		return 0, err
	}
	params := make([]db.InsertPronunciationsParams, len(rows))
	for i, r := range rows {
		params[i] = db.InsertPronunciationsParams{Lang: string(src.Lang), Normalized: r.Normalized, Phonemes: r.Phonemes,
			Confidence: r.Confidence, SourceID: srcID}
	}
	if _, err := q.InsertPronunciations(ctx, params); err != nil {
		return 0, err
	}
	if err := q.FinishRun(ctx, db.FinishRunParams{ID: runID, Status: "ok", Inserted: int32(len(rows)), Errors: []byte("{}")}); err != nil {
		return 0, err
	}
	if dryRun {
		return runID, nil
	}
	return runID, tx.Commit(ctx)
}
