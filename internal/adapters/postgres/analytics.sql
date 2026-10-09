-- name: InsertEvents :copyfrom
insert into events (ts, user_id, anon_id, type, lang, dialect, level, query, n_syllables, entry_id, result_count, latency_ms, props)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13);

-- name: AnonymizeUserEvents :execrows
update events set user_id = null, anon_id = 'deleted:' || md5(random()::text) where user_id = $1;

-- name: Summary :one
-- Window starts are computed by the caller (sqlc mangles a repeated named
-- parameter combined with interval arithmetic).
select
  (select count(distinct coalesce(e1.user_id::text, e1.anon_id)) from events e1
     where e1.ts > sqlc.arg(day_from)::timestamptz and e1.ts <= sqlc.arg(until)::timestamptz)::bigint as dau,
  (select count(distinct coalesce(e2.user_id::text, e2.anon_id)) from events e2
     where e2.ts > sqlc.arg(week_from)::timestamptz and e2.ts <= sqlc.arg(until)::timestamptz)::bigint as wau,
  (select count(distinct coalesce(e3.user_id::text, e3.anon_id)) from events e3
     where e3.ts > sqlc.arg(month_from)::timestamptz and e3.ts <= sqlc.arg(until)::timestamptz)::bigint as mau,
  (select count(*) from events e4 where e4.type = 'search' and e4.ts >= sqlc.arg(since)::timestamptz and e4.ts < sqlc.arg(until)::timestamptz)::bigint as searches,
  (select count(*) from events e5 where e5.type = 'save_item' and e5.ts >= sqlc.arg(since)::timestamptz and e5.ts < sqlc.arg(until)::timestamptz)::bigint as saves,
  (select count(*) from users u where u.created_at >= sqlc.arg(since)::timestamptz and u.created_at < sqlc.arg(until)::timestamptz)::bigint as new_users,
  coalesce((select percentile_cont(0.5) within group (order by e6.latency_ms) from events e6
            where e6.type = 'search' and e6.ts >= sqlc.arg(since)::timestamptz and e6.ts < sqlc.arg(until)::timestamptz and e6.latency_ms is not null), 0)::float8 as p50_ms,
  coalesce((select percentile_cont(0.99) within group (order by e7.latency_ms) from events e7
            where e7.type = 'search' and e7.ts >= sqlc.arg(since)::timestamptz and e7.ts < sqlc.arg(until)::timestamptz and e7.latency_ms is not null), 0)::float8 as p99_ms;

-- name: DailyActivity :many
select d::timestamptz as day, actives, searches, saves from mv_daily_activity
where d >= @since::timestamptz and d < @until::timestamptz order by d;

-- name: TopQueries :many
select coalesce(lang, '')::text as lang, query, count(*)::bigint as n
from events where type = 'search' and query is not null and ts > now() - @window_len::interval
group by 1, 2 order by n desc, query limit @lim;

-- name: RisingQueries :many
select coalesce(lang, '')::text as lang, query, c7::bigint as c7, c30::bigint as c30, coalesce(rising, 999)::float8 as rising
from mv_query_trends where c7 >= 3
order by rising desc nulls first, c7 desc, query limit @lim;

-- name: ZeroResultQueries :many
select coalesce(lang, '')::text as lang, query, count(*)::bigint as n
from events where type = 'search' and result_count = 0 and query is not null and ts > now() - @window_len::interval
group by 1, 2 order by n desc, query limit @lim;

-- name: Breakdown :many
select dim, value, n from (
  select 'lang' as dim, coalesce(lang, '?') as value, count(*) as n from events
    where type = 'search' and ts > now() - @window_len::interval group by 2
  union all
  select 'dialect', coalesce(dialect, '?'), count(*) from events
    where type = 'search' and lang = 'vi' and ts > now() - @window_len::interval group by 2
  union all
  select 'level', coalesce(level, '?'), count(*) from events
    where type = 'search' and ts > now() - @window_len::interval group by 2
  union all
  select 'n_syllables', coalesce(n_syllables::text, '?'), count(*) from events
    where type = 'search' and ts > now() - @window_len::interval group by 2
) b order by dim, n desc;

-- name: SlangRate :one
select count(*) filter (where e.is_slang)::bigint as slang, count(*)::bigint as total
from events ev join entries e on e.id = ev.entry_id
where ev.type in ('click_result', 'save_item') and ev.ts > now() - @window_len::interval;

-- name: TopSlang :many
select e.id, e.headword, e.trend_score,
       count(*) filter (where ev.type in ('click_result', 'view_word'))::bigint as views,
       count(*) filter (where ev.type = 'save_item')::bigint as saves
from events ev join entries e on e.id = ev.entry_id
where e.is_slang and ev.ts > now() - @window_len::interval
group by e.id order by saves desc, views desc, e.headword limit @lim;

-- name: SlangToBump :many
-- Slang whose save rate over the last 7 days is in the top 10% (spec:
-- auto-bump +0.2); at least 3 views, so a single save does not count.
with s as (
  select ev.entry_id,
         count(*) filter (where ev.type = 'save_item')::float8 as saves,
         greatest(count(*) filter (where ev.type in ('click_result', 'view_word')), 1)::float8 as views
  from events ev join entries e on e.id = ev.entry_id
  where e.is_slang and ev.ts > now() - interval '7 days'
  group by ev.entry_id
  having count(*) filter (where ev.type in ('click_result', 'view_word')) >= 3
     and count(*) filter (where ev.type = 'save_item') > 0
), r as (select entry_id, percent_rank() over (order by saves / views) as pr from s)
select entry_id::bigint from r where pr >= 0.9 order by entry_id;

-- name: UserActivity :many
select u.id, u.email, coalesce(u.display_name, '')::text as display_name, u.role, u.created_at, u.last_seen_at,
       (select count(*) from events ev where ev.user_id = u.id and ev.type = 'search')::bigint as searches,
       (select count(*) from rhyme_books b where b.user_id = u.id)::bigint as books,
       (select count(*) from rhyme_book_items i join rhyme_books b on b.id = i.book_id where b.user_id = u.id)::bigint as items
from users u
order by case when @sort::text = 'new' then u.created_at end desc nulls last,
         case when @sort::text = 'searches' then (select count(*) from events ev where ev.user_id = u.id and ev.type = 'search') end desc nulls last,
         u.last_seen_at desc nulls last, u.email
limit @lim;

-- name: ClaimJob :one
-- Records a run of a periodic job if its interval has passed; returns
-- whether the caller should run it.
insert into job_runs (name, last_run) values (@name, now())
on conflict (name) do update set last_run = now()
  where job_runs.last_run < now() - @every::interval
returning true;
