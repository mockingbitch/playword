-- name: UpsertSource :one
insert into sources (name, kind, lang, license, url)
values ($1, $2, $3, $4, $5)
on conflict (name) do update
  set kind = excluded.kind, lang = excluded.lang, license = excluded.license, url = excluded.url
returning id;

-- name: CreateRun :one
insert into ingest_runs (source_id, full_run) values ($1, $2) returning id;

-- name: FinishRun :exec
update ingest_runs
set finished_at = now(), status = $2, inserted = $3, updated = $4, unchanged = $5,
    skipped = $6, staled = $7, errors = $8
where id = $1;

-- name: GetKeysMeta :one
select config_id, built_at from rhyme_keys_meta where one;

-- name: UpsertRhymeConfig :exec
insert into rhyme_configs (id, families_yaml, dialects_yaml) values ($1, $2, $3)
on conflict (id) do nothing;

-- name: SetKeysMeta :exec
insert into rhyme_keys_meta (one, config_id, built_at) values (true, $1, now())
on conflict (one) do update set config_id = excluded.config_id, built_at = excluded.built_at;

-- name: MarkStale :execrows
update entries set status = 'stale', updated_at = now()
where source_id = $1 and status = 'approved' and last_seen_run is distinct from $2;

-- name: DeleteSenses :exec
delete from senses where entry_id = any(@ids::bigint[]);

-- name: DeleteSyllables :exec
delete from syllables where entry_id = any(@ids::bigint[]);

-- name: DeleteRhymeKeys :exec
delete from rhyme_keys where entry_id = any(@ids::bigint[]);

-- name: InsertSenses :copyfrom
insert into senses (entry_id, pos, gloss, examples, ord) values ($1, $2, $3, $4, $5);

-- name: InsertSyllables :copyfrom
insert into syllables (entry_id, idx, onset, medial, nucleus, coda, tone, phonemes) values ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: InsertRhymeKeys :copyfrom
insert into rhyme_keys (entry_id, dialect, level, start, n, key) values ($1, $2, $3, $4, $5, $6);

-- name: ListIndexEntries :many
select e.id, e.lang, e.headword, e.normalized, e.pos, e.is_slang, e.trend_score, e.trend_at, e.tags, e.phonetic_confidence, s.name as source,
       coalesce((select max(f.zipf) from word_freq f
                 where f.lang = e.lang and f.normalized = e.normalized), 0)::float8 as zipf
from entries e join sources s on s.id = e.source_id
where e.lang = $1 and e.status = 'approved'
order by e.id;

-- name: ListIndexSyllables :many
select y.entry_id, y.idx, y.onset, y.medial, y.nucleus, y.coda, y.tone, y.phonemes
from syllables y join entries e on e.id = y.entry_id
where e.lang = $1 and e.status = 'approved' and e.rhyme_error is null
order by y.entry_id, y.idx;

-- name: ListAllSyllables :many
select y.entry_id, e.lang, y.idx, y.onset, y.medial, y.nucleus, y.coda, y.tone, y.phonemes
from syllables y join entries e on e.id = y.entry_id
order by y.entry_id, y.idx;

-- name: ListIndexSenses :many
select y.entry_id, y.pos, y.gloss, y.examples
from senses y join entries e on e.id = y.entry_id
where e.lang = $1 and e.status = 'approved'
order by y.entry_id, y.ord;

-- name: ListVocab :many
select distinct normalized from entries where lang = $1 and status <> 'rejected';

-- name: DeleteWordFreq :exec
delete from word_freq where source_id = $1;

-- name: InsertWordFreq :copyfrom
insert into word_freq (lang, normalized, zipf, source_id) values ($1, $2, $3, $4);

-- name: ChangeFingerprint :one
select coalesce((select max(id) from ingest_runs where finished_at is not null and status in ('ok', 'partial')), 0)::bigint as run_id,
       coalesce((select built_at::text from rhyme_keys_meta where one), '')::text as keys_built;

-- name: CountSourceEntries :one
select count(*) from entries where source_id = $1;

-- name: DeletePronunciations :exec
delete from pronunciations where source_id = $1;

-- name: InsertPronunciations :copyfrom
insert into pronunciations (lang, normalized, phonemes, confidence, source_id) values ($1, $2, $3, $4, $5);

-- name: ListPronunciations :many
select normalized, phonemes from pronunciations where lang = $1 order by normalized, source_id;

-- name: LastSuccessfulRun :one
select coalesce(max(r.started_at), 'epoch'::timestamptz)::timestamptz
from ingest_runs r join sources s on s.id = r.source_id
where s.name = $1 and r.status = 'ok';
