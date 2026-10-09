// Package postgres is the storage adapter: migrations, ingest writes, rhyme
// key rebuilds and the bulk reads the in-memory index is built from.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"playword/internal/adapters/postgres/db"
	"playword/internal/domain/entry"
	"playword/internal/domain/rhyme"
	"playword/internal/domain/rhyme/en"
	"playword/internal/ports"
	"playword/migrations"
	"playword/pkg/vnphon"
)

// NotifyChannel is where index builders listen for new data (ADR-002 §5).
const NotifyChannel = "playword_index"

// Store is the PostgreSQL adapter.
type Store struct {
	pool *pgxpool.Pool
}

// Open connects and pings the database.
func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

// Close closes the connection pool.
func (s *Store) Close() { s.pool.Close() }

// Pool exposes the pool for packages that run their own queries.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Migrate runs goose with the embedded migrations: command is up, down,
// status, … as goose understands it.
func (s *Store) Migrate(ctx context.Context, command string, args ...string) error {
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	conn := stdlib.OpenDBFromPool(s.pool)
	defer func() { _ = conn.Close() }()
	return goose.RunContext(ctx, command, conn, ".", args...)
}

// querier is satisfied by both a pool transaction and a nested savepoint.
type querier interface {
	db.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
	CopyFrom(ctx context.Context, table pgx.Identifier, cols []string, src pgx.CopyFromSource) (int64, error)
}

// StartRun registers the source, checks the rhyme config and opens a run.
func (s *Store) StartRun(ctx context.Context, o ports.RunOptions) (ports.IngestRun, error) {
	q := db.New(s.pool)
	srcID, err := q.UpsertSource(ctx, db.UpsertSourceParams{
		Name: o.Source.Name(), Kind: string(o.Source.Kind()), Lang: string(o.Source.Lang()),
		License: o.Source.License(), Url: pgtype.Text{String: o.Source.URL(), Valid: true},
	})
	if err != nil {
		return nil, err
	}
	if err := ensureKeysConfig(ctx, q, o.Keys); err != nil {
		return nil, err
	}

	r := &run{store: s, sourceID: srcID, opts: o, conn: s.pool}
	if !o.DryRun && o.Engine != nil {
		existing, err := q.CountSourceEntries(ctx, srcID)
		if err != nil {
			return nil, err
		}
		r.deferKeys = int(existing)*2 < o.Expected
	}
	if o.DryRun {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		r.outer, r.conn = tx, tx
	}
	r.id, err = db.New(r.conn).CreateRun(ctx, db.CreateRunParams{SourceID: srcID, FullRun: o.FullRun})
	if err != nil {
		r.rollback(ctx)
		return nil, err
	}
	return r, nil
}

// ensureKeysConfig records the config on first use and refuses to mix keys
// from two configs afterwards (ADR-003 D1).
func ensureKeysConfig(ctx context.Context, q *db.Queries, k ports.KeysConfig) error {
	meta, err := q.GetKeysMeta(ctx)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := q.UpsertRhymeConfig(ctx, db.UpsertRhymeConfigParams{ID: k.ID, FamiliesYaml: k.FamiliesYAML, DialectsYaml: k.DialectsYAML}); err != nil {
			return err
		}
		return q.SetKeysMeta(ctx, k.ID)
	case err != nil:
		return err
	case meta.ConfigID != k.ID:
		return fmt.Errorf("%w (stored %.12s…, running %.12s…)", ports.ErrKeysConfigMismatch, meta.ConfigID, k.ID)
	}
	return nil
}

type run struct {
	store     *Store
	id        int64
	sourceID  int32
	opts      ports.RunOptions
	conn      querier
	outer     pgx.Tx // dry runs only
	deferKeys bool   // rhyme keys are rebuilt in bulk by Finish
}

func (r *run) ID() int64 { return r.id }

func (r *run) rollback(ctx context.Context) {
	if r.outer != nil {
		_ = r.outer.Rollback(ctx)
	}
}

const stageDDL = `
create temp table if not exists stage (
  idx int, lang text, headword text, normalized text, n_syllables smallint, pos text,
  is_slang boolean, tags text[], source_ref text, rhyme_error text, content_hash bytea,
  phonetic_confidence real, respelling text
) on commit drop;
truncate stage`

// WriteBatch upserts one batch in its own transaction (a savepoint in dry
// runs): unchanged rows only get last_seen_run, changed rows are rewritten
// together with their senses, syllables and rhyme keys.
func (r *run) WriteBatch(ctx context.Context, batch []ports.PreparedEntry) (ports.BatchCounts, error) {
	var c ports.BatchCounts
	tx, err := r.conn.Begin(ctx)
	if err != nil {
		return c, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, stageDDL); err != nil {
		return c, err
	}
	rows := make([][]any, len(batch))
	for i, p := range batch {
		var rhymeErr *string
		if p.RhymeError != "" {
			rhymeErr = &p.RhymeError
		}
		tags := p.Entry.Tags
		if tags == nil {
			tags = []string{}
		}
		conf := p.Confidence
		if conf == 0 {
			conf = 1
		}
		rows[i] = []any{i, string(p.Entry.Lang), p.Entry.Headword, p.Normalized, p.NSyllables,
			nullable(p.Entry.POS), p.Entry.IsSlang, tags, nullable(p.Entry.SourceRef), rhymeErr, p.Hash, float32(conf),
			nullable(p.Entry.Respelling)}
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"stage"},
		[]string{"idx", "lang", "headword", "normalized", "n_syllables", "pos", "is_slang", "tags", "source_ref", "rhyme_error", "content_hash", "phonetic_confidence", "respelling"},
		pgx.CopyFromRows(rows)); err != nil {
		return c, err
	}

	tag, err := tx.Exec(ctx, `
		update entries e
		set last_seen_run = $1, status = case when e.status = 'stale' then 'approved' else e.status end
		from stage s
		where e.lang = s.lang and e.normalized = s.normalized and e.source_id = $2
		  and e.content_hash = s.content_hash`, r.id, r.sourceID)
	if err != nil {
		return c, err
	}
	c.Unchanged = int(tag.RowsAffected())

	changed, err := tx.Query(ctx, `
		insert into entries as e (lang, headword, normalized, n_syllables, pos, is_slang, tags,
		                          source_id, source_ref, rhyme_error, content_hash, last_seen_run, phonetic_confidence, respelling)
		select s.lang, s.headword, s.normalized, s.n_syllables, s.pos, s.is_slang, s.tags,
		       $2, s.source_ref, s.rhyme_error, s.content_hash, $1, s.phonetic_confidence, s.respelling
		from stage s
		where not exists (select 1 from entries x where x.lang = s.lang and x.normalized = s.normalized
		                  and x.source_id = $2 and x.content_hash = s.content_hash)
		on conflict (lang, normalized, source_id) do update set
		  headword = excluded.headword, n_syllables = excluded.n_syllables, pos = excluded.pos,
		  is_slang = excluded.is_slang, tags = excluded.tags, source_ref = excluded.source_ref,
		  rhyme_error = excluded.rhyme_error, content_hash = excluded.content_hash,
		  phonetic_confidence = excluded.phonetic_confidence, respelling = excluded.respelling,
		  last_seen_run = excluded.last_seen_run, updated_at = now(),
		  status = case when e.status = 'stale' then 'approved' else e.status end
		returning e.id, e.normalized, (e.xmax = 0)`, r.id, r.sourceID)
	if err != nil {
		return c, err
	}
	byNorm := make(map[string]int, len(batch))
	for i, p := range batch {
		byNorm[p.Normalized] = i
	}
	type changedRow struct {
		id  int64
		idx int
	}
	var ids []int64
	var rewritten []changedRow
	for changed.Next() {
		var id int64
		var norm string
		var inserted bool
		if err := changed.Scan(&id, &norm, &inserted); err != nil {
			changed.Close()
			return c, err
		}
		if inserted {
			c.Inserted++
		} else {
			c.Updated++
		}
		ids = append(ids, id)
		rewritten = append(rewritten, changedRow{id, byNorm[norm]})
	}
	if err := changed.Err(); err != nil {
		return c, err
	}

	q := db.New(tx)
	dels := []func(context.Context, []int64) error{q.DeleteSenses, q.DeleteSyllables}
	if !r.deferKeys {
		dels = append(dels, q.DeleteRhymeKeys)
	}
	for _, del := range dels {
		if err := del(ctx, ids); err != nil {
			return c, err
		}
	}
	var senses []db.InsertSensesParams
	var syls []db.InsertSyllablesParams
	var keys []db.InsertRhymeKeysParams
	for _, ch := range rewritten {
		p := batch[ch.idx]
		for i, sn := range p.Entry.Senses {
			ex := sn.Examples
			if ex == nil {
				ex = []string{}
			}
			senses = append(senses, db.InsertSensesParams{EntryID: ch.id, Pos: nullable(sn.POS), Gloss: sn.Gloss, Examples: ex, Ord: int16(i)})
		}
		for i, s := range p.Syllables {
			syls = append(syls, db.InsertSyllablesParams{EntryID: ch.id, Idx: int16(i), Onset: s.Onset, Medial: s.Medial,
				Nucleus: s.Nucleus, Coda: s.Coda, Tone: int16(s.Tone), Phonemes: []string{}})
		}
		for i, sg := range p.Segs {
			syls = append(syls, db.InsertSyllablesParams{EntryID: ch.id, Idx: int16(i), Phonemes: sg.Phones()})
		}
		if !r.deferKeys {
			keys = append(keys, keyRows(ch.id, p.Keys)...)
		}
	}
	if _, err := q.InsertSenses(ctx, senses); err != nil {
		return c, err
	}
	if _, err := q.InsertSyllables(ctx, syls); err != nil {
		return c, err
	}
	if _, err := q.InsertRhymeKeys(ctx, keys); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}

func keyRows(id int64, keys []rhyme.WindowKey) []db.InsertRhymeKeysParams {
	out := make([]db.InsertRhymeKeysParams, len(keys))
	for i, k := range keys {
		out[i] = db.InsertRhymeKeysParams{EntryID: id, Dialect: string(k.Dialect), Level: k.Level.String(),
			Start: int16(k.Start), N: int16(k.N), Key: k.Key}
	}
	return out
}

func (r *run) Finish(ctx context.Context, res ports.RunResult) (ports.FinishInfo, error) {
	defer r.rollback(ctx) // no-op once committed
	var info ports.FinishInfo
	q := db.New(r.conn)
	if res.MarkStale && res.Status == "ok" {
		n, err := q.MarkStale(ctx, db.MarkStaleParams{SourceID: r.sourceID, LastSeenRun: pgtype.Int8{Int64: r.id, Valid: true}})
		if err != nil {
			return info, err
		}
		info.Staled = int(n)
	}
	errs := res.Errors
	if len(errs) == 0 {
		errs = []byte("{}")
	}
	if err := q.FinishRun(ctx, db.FinishRunParams{
		ID: r.id, Status: res.Status, Inserted: int32(res.Counts.Inserted), Updated: int32(res.Counts.Updated),
		Unchanged: int32(res.Counts.Unchanged), Skipped: int32(res.Skipped), Staled: int32(info.Staled), Errors: errs,
	}); err != nil {
		return info, err
	}
	if r.opts.DryRun {
		return info, r.outer.Rollback(ctx)
	}
	if r.deferKeys && res.Status != "failed" {
		if _, err := r.store.RebuildKeys(ctx, r.opts.Engine); err != nil {
			return info, fmt.Errorf("rebuild rhyme keys after bulk load: %w", err)
		}
		info.KeysRebuilt = true // RebuildKeys notified index builders
		return info, nil
	}
	if res.Counts.Inserted+res.Counts.Updated+info.Staled > 0 {
		if _, err := r.conn.Exec(ctx, "select pg_notify($1, $2)", NotifyChannel, strconv.FormatInt(r.id, 10)); err != nil {
			return info, err
		}
	}
	return info, nil
}

// RebuildKeys recomputes every stored rhyme key with e and swaps the table
// in one transaction, so readers never see a half-written table (ADR-003 D1).
func (s *Store) RebuildKeys(ctx context.Context, e *rhyme.Engine) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)

	sylRows, err := q.ListAllSyllables(ctx)
	if err != nil {
		return 0, err
	}
	var keys [][]any
	for i := 0; i < len(sylRows); {
		id, lang := sylRows[i].EntryID, sylRows[i].Lang
		var p []vnphon.Syllable
		var phones [][]string
		for ; i < len(sylRows) && sylRows[i].EntryID == id; i++ {
			r := sylRows[i]
			p = append(p, vnphon.Syllable{Onset: r.Onset, Medial: r.Medial, Nucleus: r.Nucleus, Coda: r.Coda, Tone: int(r.Tone)})
			phones = append(phones, r.Phonemes)
		}
		var wk []rhyme.WindowKey
		if entry.Lang(lang) == entry.EN {
			segs, err := en.SegmentsFromRows(phones)
			if err != nil {
				return 0, fmt.Errorf("entry %d: %w", id, err)
			}
			wk = rhyme.WindowKeysEN(segs)
		} else {
			wk = e.WindowKeys(p)
		}
		for _, k := range keyRows(id, wk) {
			keys = append(keys, []any{k.EntryID, k.Dialect, k.Level, k.Start, k.N, k.Key})
		}
	}

	if _, err := tx.Exec(ctx, `
		drop table if exists rhyme_keys_new;
		create table rhyme_keys_new (like rhyme_keys including defaults)`); err != nil {
		return 0, err
	}
	n, err := tx.CopyFrom(ctx, pgx.Identifier{"rhyme_keys_new"},
		[]string{"entry_id", "dialect", "level", "start", "n", "key"}, pgx.CopyFromRows(keys))
	if err != nil {
		return 0, err
	}
	k := ports.KeysConfigOf(e)
	if _, err := tx.Exec(ctx, `
		alter table rhyme_keys_new add constraint rhyme_keys_new_pkey primary key (entry_id, dialect, level, start, n);
		alter table rhyme_keys_new add constraint rhyme_keys_entry_id_fkey
		  foreign key (entry_id) references entries on delete cascade;
		create index rhyme_keys_new_lookup on rhyme_keys_new (dialect, level, n, key);
		drop table rhyme_keys;
		alter table rhyme_keys_new rename to rhyme_keys;
		alter table rhyme_keys rename constraint rhyme_keys_new_pkey to rhyme_keys_pkey;
		alter index rhyme_keys_new_lookup rename to rhyme_keys_lookup`); err != nil {
		return 0, err
	}
	if err := q.UpsertRhymeConfig(ctx, db.UpsertRhymeConfigParams{ID: k.ID, FamiliesYaml: k.FamiliesYAML, DialectsYaml: k.DialectsYAML}); err != nil {
		return 0, err
	}
	if err := q.SetKeysMeta(ctx, k.ID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, "select pg_notify($1, 'rebuild-keys')", NotifyChannel); err != nil {
		return 0, err
	}
	return n, tx.Commit(ctx)
}

// LoadIndex reads everything the in-memory index needs, every language.
func (s *Store) LoadIndex(ctx context.Context) (*ports.IndexData, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)

	d := &ports.IndexData{Syllables: map[int64][]vnphon.Syllable{}, Segments: map[int64][]en.Seg{},
		Senses: map[int64][]entry.Sense{}, Pronunciations: map[string][]string{}}
	meta, err := q.GetKeysMeta(ctx)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	d.ConfigID = meta.ConfigID

	for _, lang := range []entry.Lang{entry.VI, entry.EN} {
		l := string(lang)
		ents, err := q.ListIndexEntries(ctx, l)
		if err != nil {
			return nil, err
		}
		for _, e := range ents {
			d.Entries = append(d.Entries, ports.IndexEntry{ID: e.ID, Lang: lang, Headword: e.Headword, Normalized: e.Normalized,
				POS: e.Pos.String, Source: e.Source, IsSlang: e.IsSlang, Tags: e.Tags, Trend: decayed(e.TrendScore, e.TrendAt), Zipf: e.Zipf,
				Confidence: math.Round(float64(e.PhoneticConfidence)*1000) / 1000}) // real is float32
		}
		syls, err := q.ListIndexSyllables(ctx, l)
		if err != nil {
			return nil, err
		}
		for i := 0; i < len(syls); {
			id := syls[i].EntryID
			var rows [][]string
			for ; i < len(syls) && syls[i].EntryID == id; i++ {
				r := syls[i]
				if lang == entry.EN {
					rows = append(rows, r.Phonemes)
				} else {
					d.Syllables[id] = append(d.Syllables[id],
						vnphon.Syllable{Onset: r.Onset, Medial: r.Medial, Nucleus: r.Nucleus, Coda: r.Coda, Tone: int(r.Tone)})
				}
			}
			if lang == entry.EN {
				if segs, err := en.SegmentsFromRows(rows); err == nil {
					d.Segments[id] = segs
				}
			}
		}
		senses, err := q.ListIndexSenses(ctx, l)
		if err != nil {
			return nil, err
		}
		for _, r := range senses {
			d.Senses[r.EntryID] = append(d.Senses[r.EntryID], entry.Sense{POS: r.Pos.String, Gloss: r.Gloss, Examples: r.Examples})
		}
	}
	prons, err := q.ListPronunciations(ctx, string(entry.EN))
	if err != nil {
		return nil, err
	}
	for _, p := range prons {
		if _, dup := d.Pronunciations[p.Normalized]; !dup {
			d.Pronunciations[p.Normalized] = p.Phonemes
		}
	}
	return d, nil
}

// Pronunciations returns lowercase word -> phones for lang (first source wins).
func (s *Store) Pronunciations(ctx context.Context, lang entry.Lang) (map[string][]string, error) {
	rows, err := db.New(s.pool).ListPronunciations(ctx, string(lang))
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(rows))
	for _, r := range rows {
		if _, dup := out[r.Normalized]; !dup {
			out[r.Normalized] = r.Phonemes
		}
	}
	return out, nil
}

// WaitForChange blocks until an ingest run or key rebuild notifies, or ctx
// ends; index holders call it in a loop.
func (s *Store) WaitForChange(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "listen "+NotifyChannel); err != nil {
		return err
	}
	_, err = conn.Conn().WaitForNotification(ctx)
	_, _ = conn.Exec(context.WithoutCancel(ctx), "unlisten "+NotifyChannel)
	return err
}

func nullable(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

var (
	_ ports.IngestStore = (*Store)(nil)
	_ ports.IndexLoader = (*Store)(nil)
)

// WaitForChangeOrPoll waits for a NOTIFY; every interval it also compares
// the latest finished ingest run and key rebuild with the last seen ones, in
// case a notification was missed (ADR-002 §5).
func (s *Store) WaitForChangeOrPoll(interval time.Duration) func(context.Context) error {
	var last db.ChangeFingerprintRow
	if fp, err := db.New(s.pool).ChangeFingerprint(context.Background()); err == nil {
		last = fp
	}
	return func(ctx context.Context) error {
		for {
			wctx, cancel := context.WithTimeout(ctx, interval)
			err := s.WaitForChange(wctx)
			cancel()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fp, ferr := db.New(s.pool).ChangeFingerprint(ctx)
			if ferr != nil {
				return ferr
			}
			changed := fp != last
			last = fp
			if err == nil || changed {
				return nil
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				return err
			}
		}
	}
}

// decayed is a trend score after its 90-day half-life (spec §2.2).
func decayed(score float64, at pgtype.Timestamptz) float64 {
	if !at.Valid || score == 0 {
		return score
	}
	return rhyme.DecayTrend(score, time.Since(at.Time))
}

// LastSuccessfulRun is when the latest ok run of a source started (zero if
// none), for conditional downloads.
func (s *Store) LastSuccessfulRun(ctx context.Context, source string) (time.Time, error) {
	t, err := db.New(s.pool).LastSuccessfulRun(ctx, source)
	if err != nil || !t.Valid || t.Time.Unix() == 0 {
		return time.Time{}, err
	}
	return t.Time, nil
}

// TryLock runs fn while holding a session-level advisory lock named key; if
// another node holds it, fn is skipped and ran is false (ADR-003 D9).
func (s *Store) TryLock(ctx context.Context, key string, fn func() error) (ran bool, err error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, "select pg_try_advisory_lock(hashtext($1))", key).Scan(&got); err != nil || !got {
		return false, err
	}
	defer func() { _, _ = conn.Exec(context.WithoutCancel(ctx), "select pg_advisory_unlock(hashtext($1))", key) }()
	return true, fn()
}
