-- +goose Up
-- Dictionary core: spec §3 with the ADR-003 amendments (D1–D6).
create extension if not exists pg_trgm;

create table sources (
  id      serial primary key,
  name    text unique not null,
  kind    text not null default 'dictionary',  -- dictionary|frequency|pronunciation|slang (D2)
  lang    text not null,
  license text not null,
  url     text
);

create table ingest_runs (
  id          bigserial primary key,
  source_id   int not null references sources(id),
  started_at  timestamptz not null default now(),
  finished_at timestamptz,
  status      text not null default 'running',  -- running|ok|partial|failed|dry_run
  full_run    boolean not null default true,
  inserted    int not null default 0,
  updated     int not null default 0,
  unchanged   int not null default 0,
  skipped     int not null default 0,
  staled      int not null default 0,
  errors      jsonb not null default '{}'
);

create table entries (
  id            bigserial primary key,
  lang          text not null,
  headword      text not null,
  normalized    text not null,                  -- NFC lowercase, single spaces
  n_syllables   smallint not null,
  pos           text,
  is_slang      boolean not null default false,
  status        text not null default 'approved', -- approved|pending|rejected|stale (D5)
  trend_score   double precision not null default 0,
  tags          text[] not null default '{}',
  source_id     int not null references sources(id),
  source_ref    text,
  rhyme_error   text,                           -- why the phrase has no rhyme keys (D4)
  content_hash  bytea not null,                 -- idempotent re-ingest (D6)
  last_seen_run bigint references ingest_runs(id),
  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now(),
  unique (lang, normalized, source_id)
);
create index entries_trgm on entries using gin (normalized gin_trgm_ops);
create index entries_lang_status on entries (lang, status);

create table senses (
  id       bigserial primary key,
  entry_id bigint not null references entries on delete cascade,
  pos      text,
  gloss    text not null,
  examples text[] not null default '{}',
  ord      smallint not null default 0
);
create index senses_entry on senses (entry_id, ord);

create table syllables (
  entry_id bigint references entries on delete cascade,
  idx      smallint,
  onset    text not null,
  medial   text not null,
  nucleus  text not null,
  coda     text not null,
  tone     smallint not null,
  phonemes text[] not null default '{}',
  primary key (entry_id, idx)
);

-- Rhyme keys are derived from syllables + YAML; they are tied to the config
-- that produced them (ADR-003 D1).
create table rhyme_configs (
  id            text primary key,               -- sha256(families || dialects)
  families_yaml text not null,
  dialects_yaml text not null,
  created_at    timestamptz not null default now()
);
create table rhyme_keys_meta (
  one       boolean primary key default true check (one),
  config_id text not null references rhyme_configs(id),
  built_at  timestamptz not null default now()
);
create table rhyme_keys (
  entry_id bigint not null references entries on delete cascade,
  dialect  text not null,
  level    text not null,
  start    smallint not null,
  n        smallint not null,
  key      text not null,
  primary key (entry_id, dialect, level, start, n)
);
create index rhyme_keys_lookup on rhyme_keys (dialect, level, n, key);

-- Enrichment sources that carry no definitions (D2).
create table word_freq (
  lang       text not null,
  normalized text not null,
  zipf       double precision not null,
  source_id  int not null references sources(id),
  primary key (lang, normalized, source_id)
);
create table pronunciations (
  lang       text not null,
  normalized text not null,
  phonemes   text[] not null,
  confidence real not null default 1,
  source_id  int not null references sources(id),
  primary key (lang, normalized, source_id)
);

-- +goose Down
drop table pronunciations, word_freq, rhyme_keys, rhyme_keys_meta, rhyme_configs,
  syllables, senses, entries, ingest_runs, sources;
