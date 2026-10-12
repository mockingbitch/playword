-- name: CreateUser :one
insert into users (email, password_hash, display_name, email_verified_at)
values (@email, nullif(@password_hash::text, ''), @display_name, case when @verified::bool then now() end)
returning id, email, coalesce(display_name, '')::text as display_name, default_dialect, default_lang, default_level, role, created_at;

-- name: GetUserByEmail :one
select id, email, coalesce(password_hash, '')::text as password_hash, coalesce(display_name, '')::text as display_name, default_dialect, default_lang,
       default_level, role, failed_logins, locked_until, created_at
from users where email = $1;

-- name: GetUserByID :one
select id, email, coalesce(display_name, '')::text as display_name, default_dialect, default_lang, default_level, role, created_at
from users where id = $1;

-- name: RecordLoginFailure :one
update users set failed_logins = failed_logins + 1,
  locked_until = case when failed_logins + 1 >= @max_failures::int then now() + @lock_for::interval else locked_until end
where id = $1
returning failed_logins, locked_until;

-- name: ResetLoginFailures :exec
update users set failed_logins = 0, locked_until = null, last_seen_at = now() where id = $1;

-- name: UpdateUserSettings :exec
update users set display_name = nullif(@display_name::text, ''), default_dialect = @default_dialect,
  default_lang = @default_lang, default_level = @default_level
where id = @id;

-- name: DeleteUser :exec
delete from users where id = $1;

-- name: SetUserRole :exec
update users set role = $2 where email = $1;

-- name: CreateSession :exec
insert into sessions (id, user_id, expires_at, ip_hash, ua) values ($1, $2, $3, $4, $5);

-- name: GetSessionUser :one
select u.id, u.email, coalesce(u.display_name, '')::text as display_name, u.default_dialect, u.default_lang,
       u.default_level, u.role, u.created_at, s.expires_at
from sessions s join users u on u.id = s.user_id
where s.id = $1 and s.expires_at > now();

-- name: DeleteSession :exec
delete from sessions where id = $1;

-- name: DeleteExpiredSessions :execrows
delete from sessions where expires_at <= now();

-- name: CreateBook :one
insert into rhyme_books (user_id, title, description, is_public) values ($1, $2, nullif(@description::text, ''), $3)
returning id, created_at, updated_at;

-- name: ListBooks :many
select b.id, b.user_id, b.title, coalesce(b.description, '')::text as description, b.is_public, b.created_at, b.updated_at,
       (select count(*) from rhyme_book_items i where i.book_id = b.id)::int as item_count
from rhyme_books b where b.user_id = $1 order by b.updated_at desc, b.id;

-- name: GetBook :one
select b.id, b.user_id, b.title, coalesce(b.description, '')::text as description, b.is_public, b.created_at, b.updated_at, b.lyrics,
       (select count(*) from rhyme_book_items i where i.book_id = b.id)::int as item_count
from rhyme_books b where b.id = $1;

-- name: UpdateBook :execrows
update rhyme_books set title = $3, description = nullif(@description::text, ''), is_public = $4, updated_at = now()
where id = $1 and user_id = $2;

-- name: SetBookLyrics :execrows
update rhyme_books set lyrics = $3, updated_at = now() where id = $1 and user_id = $2;

-- name: TouchBook :exec
update rhyme_books set updated_at = now() where id = $1;

-- name: DeleteBook :execrows
delete from rhyme_books where id = $1 and user_id = $2;

-- name: ListItems :many
select i.id, i.book_id, coalesce(i.entry_id, 0)::bigint as entry_id,
       coalesce(e.headword, i.custom_text, '')::text as text, coalesce(i.custom_note, '')::text as note,
       i.lang, coalesce(i.dialect, '')::text as dialect, coalesce(i.level, '')::text as level,
       coalesce(i.matched_from, '')::text as matched_from, i.ord, i.created_at
from rhyme_book_items i left join entries e on e.id = i.entry_id
where i.book_id = $1 order by i.ord, i.id;

-- name: AddItem :one
insert into rhyme_book_items (book_id, entry_id, custom_text, custom_note, lang, dialect, level, matched_from, ord)
values (@book_id, nullif(@entry_id::bigint, 0), nullif(@custom_text::text, ''), nullif(@note::text, ''), @lang,
        nullif(@dialect::text, ''), nullif(@level::text, ''), nullif(@matched_from::text, ''),
        coalesce((select max(ord) + 1 from rhyme_book_items where book_id = @book_id), 0))
returning id, ord, created_at;

-- name: HasEntryItem :one
select exists(select 1 from rhyme_book_items where book_id = $1 and entry_id = $2);

-- name: UpdateItemNote :execrows
update rhyme_book_items set custom_note = nullif(@note::text, '') where id = @id and book_id = @book_id;

-- name: DeleteItem :execrows
delete from rhyme_book_items where id = $1 and book_id = $2;

-- name: SetItemOrd :execrows
update rhyme_book_items set ord = $3 where id = $1 and book_id = $2;

-- name: EntryExists :one
select exists(select 1 from entries where id = $1 and status = 'approved');

-- name: GetUserByIdentity :one
select u.id from user_identities i join users u on u.id = i.user_id where i.provider = $1 and i.subject = $2;

-- name: CreateIdentity :exec
insert into user_identities (provider, subject, user_id, email) values ($1, $2, $3, $4);

-- name: ListIdentities :many
select provider from user_identities where user_id = $1 order by provider;

-- name: MarkEmailVerified :exec
update users set email_verified_at = coalesce(email_verified_at, now()) where id = $1;

-- name: SetPasswordHash :exec
update users set password_hash = nullif(@password_hash::text, ''), failed_logins = 0, locked_until = null where id = @id;

-- name: DeleteUserSessions :execrows
delete from sessions where user_id = @user_id and id <> @keep::text;
