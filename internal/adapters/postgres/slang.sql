-- name: CreateSubmission :one
insert into slang_submissions (headword, lang, gloss, example, respelling, tags, submitted_by)
values (@headword, @lang, @gloss, nullif(@example::text, ''), nullif(@respelling::text, ''), @tags, nullif(@submitted_by::text, ''))
returning id, created_at;

-- name: ListSubmissions :many
select id, headword, lang, gloss, coalesce(example, '')::text as example, coalesce(respelling, '')::text as respelling,
       tags, coalesce(submitted_by, '')::text as submitted_by, status, coalesce(entry_id, 0)::bigint as entry_id,
       created_at, reviewed_at, coalesce(reviewed_by, '')::text as reviewed_by
from slang_submissions where status = $1 order by created_at, id limit $2;

-- name: GetSubmission :one
select id, headword, lang, gloss, coalesce(example, '')::text as example, coalesce(respelling, '')::text as respelling,
       tags, coalesce(submitted_by, '')::text as submitted_by, status, coalesce(entry_id, 0)::bigint as entry_id,
       created_at, reviewed_at, coalesce(reviewed_by, '')::text as reviewed_by
from slang_submissions where id = $1;

-- name: ReviewSubmission :execrows
update slang_submissions set status = @status, entry_id = nullif(@entry_id::bigint, 0), reviewed_at = now(),
  reviewed_by = @reviewed_by
where id = @id and status = 'pending';

-- name: EntryIDBySource :one
select e.id from entries e join sources s on s.id = e.source_id
where e.lang = $1 and e.normalized = $2 and s.name = $3;

-- name: SetTrend :exec
update entries set trend_score = $2, trend_at = now(), updated_at = now() where id = $1;

-- name: BumpTrend :one
update entries set
  trend_score = trend_score * power(0.5, extract(epoch from now() - coalesce(trend_at, now())) / 86400.0 / 90.0) + @delta::float8,
  trend_at = now()
where id = @id and is_slang
returning trend_score;

-- name: SubmissionExists :one
select exists(select 1 from slang_submissions where lang = $1 and headword = $2 and submitted_by = $3);
