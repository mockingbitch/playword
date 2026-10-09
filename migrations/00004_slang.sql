-- +goose Up
-- Slang (spec §2.2, M6). Every slang starts as a pending submission; an
-- admin approves it into entries (source "community"). respelling (Q8) is
-- how a loanword is pronounced in Vietnamese ("chill" -> "chiu") so it can
-- rhyme; trend_at is when trend_score was last set, for the 90-day decay.
create table slang_submissions (
  id           bigserial primary key,
  headword     text not null,
  lang         text not null default 'vi',
  gloss        text not null,
  example      text,
  respelling   text,
  tags         text[] not null default '{}',
  submitted_by text,
  status       text not null default 'pending',  -- pending|approved|rejected
  entry_id     bigint references entries,       -- set on approval
  created_at   timestamptz not null default now(),
  reviewed_at  timestamptz,
  reviewed_by  text
);
create index slang_submissions_status on slang_submissions (status, created_at);

alter table entries add column respelling text;
alter table entries add column trend_at timestamptz;

-- +goose Down
alter table entries drop column trend_at;
alter table entries drop column respelling;
drop table slang_submissions;
