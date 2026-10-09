-- +goose Up
-- Accounts and rhyme books (spec addendum §3, M5). Sessions store a SHA-256
-- of the cookie token, never the token itself (ADR-003 D7).
create extension if not exists citext;

create table users (
  id               uuid primary key default gen_random_uuid(),
  email            citext unique not null,
  password_hash    text not null,                 -- argon2id, PHC string
  display_name     text,
  default_dialect  text not null default 'bac',
  default_lang     text not null default 'vi',
  default_level    text not null default 'L25',
  role             text not null default 'user',  -- user|admin
  failed_logins    int not null default 0,        -- consecutive; reset on success
  locked_until     timestamptz,                   -- 15 min after 10 failures
  created_at       timestamptz not null default now(),
  last_seen_at     timestamptz
);

create table sessions (
  id         text primary key,                    -- sha256(cookie token), hex
  user_id    uuid not null references users on delete cascade,
  expires_at timestamptz not null,
  ip_hash    text,
  ua         text,
  created_at timestamptz not null default now()
);
create index sessions_user on sessions(user_id);

create table rhyme_books (
  id          uuid primary key default gen_random_uuid(),
  user_id     uuid not null references users on delete cascade,
  title       text not null,
  description text,
  is_public   boolean not null default false,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now()
);
create index rhyme_books_user on rhyme_books(user_id, updated_at desc);

create table rhyme_book_items (
  id           bigserial primary key,
  book_id      uuid not null references rhyme_books on delete cascade,
  entry_id     bigint references entries,     -- entries are never hard-deleted (ADR-003 D5)
  custom_text  text,
  custom_note  text,
  lang         text not null default 'vi',
  dialect      text,
  level        text,
  matched_from text,
  ord          int not null default 0,
  created_at   timestamptz not null default now(),
  check (entry_id is not null or custom_text is not null)
);
create index rbi_book on rhyme_book_items(book_id, ord, id);

-- +goose Down
drop table rhyme_book_items, rhyme_books, sessions, users;
