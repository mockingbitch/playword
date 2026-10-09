-- +goose Up
-- External sign-in (Google first). An account may have no password when it
-- was created through a provider; email_verified_at is set when a provider
-- vouched for the address.
create table user_identities (
  provider   text not null,            -- google
  subject    text not null,            -- the provider's stable user id ("sub")
  user_id    uuid not null references users on delete cascade,
  email      citext,
  created_at timestamptz not null default now(),
  primary key (provider, subject)
);
create index user_identities_user on user_identities(user_id);

alter table users alter column password_hash drop not null;
alter table users add column email_verified_at timestamptz;

-- +goose Down
alter table users drop column email_verified_at;
update users set password_hash = '!' where password_hash is null;
alter table users alter column password_hash set not null;
drop table user_identities;
