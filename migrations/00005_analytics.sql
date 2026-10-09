-- +goose Up
-- Analytics (spec addendum §3, M7). Append-only, no raw PII: users by id or
-- a random anon cookie; no IP addresses at all.
create table events (
  id          bigserial,
  ts          timestamptz not null default now(),
  user_id     uuid,
  anon_id     text,
  type        text not null,  -- search|view_word|click_result|save_item|submit_slang|login
  lang        text,
  dialect     text,
  level       text,
  query       text,
  n_syllables smallint,
  entry_id    bigint,
  result_count int,
  latency_ms  int,
  props       jsonb not null default '{}',
  primary key (id, ts)
) partition by range (ts);
-- Rows outside every monthly partition land here instead of failing.
create table events_default partition of events default;
create index events_ts on events(ts);
create index events_type_ts on events(type, ts);
create index events_query on events(lang, query, ts) where type = 'search';
create index events_user on events(user_id) where user_id is not null;

-- +goose StatementBegin
-- playword_ensure_event_partitions creates monthly partitions from this
-- month to months_ahead months on (D8: no pg_partman).
create function playword_ensure_event_partitions(months_ahead int) returns int language plpgsql as $$
declare
  m date := date_trunc('month', now())::date;
  made int := 0;
  name text;
begin
  for i in 0..months_ahead loop
    name := 'events_' || to_char(m, 'YYYY_MM');
    if to_regclass(name) is null then
      execute format('create table %I partition of events for values from (%L) to (%L)', name, m, (m + interval '1 month')::date);
      made := made + 1;
    end if;
    m := (m + interval '1 month')::date;
  end loop;
  return made;
end $$;
-- +goose StatementEnd

-- +goose StatementBegin
-- playword_drop_old_event_partitions drops monthly partitions whose whole
-- month is older than keep_days (retention: 180 days).
create function playword_drop_old_event_partitions(keep_days int) returns int language plpgsql as $$
declare
  r record;
  dropped int := 0;
  month_end date;
begin
  for r in select c.relname from pg_inherits i join pg_class c on c.oid = i.inhrelid
           where i.inhparent = 'events'::regclass and c.relname ~ '^events_\d{4}_\d{2}$' loop
    month_end := (to_date(substring(r.relname from 8), 'YYYY_MM') + interval '1 month')::date;
    if month_end < (now() - make_interval(days => keep_days))::date then
      execute format('drop table %I', r.relname);
      dropped := dropped + 1;
    end if;
  end loop;
  delete from events_default where ts < now() - make_interval(days => keep_days);
  return dropped;
end $$;
-- +goose StatementEnd

select playword_ensure_event_partitions(2);

create materialized view mv_daily_activity as
  select date_trunc('day', ts) as d,
         count(distinct coalesce(user_id::text, anon_id)) as actives,
         count(*) filter (where type = 'search') as searches,
         count(*) filter (where type = 'save_item') as saves
  from events group by 1;
create unique index mv_daily_activity_d on mv_daily_activity(d);

-- Rising queries: last 7 days against the 30 days before, scaled to a week.
create materialized view mv_query_trends as
  select lang, query,
    count(*) filter (where ts > now() - interval '7 days') as c7,
    count(*) filter (where ts between now() - interval '37 days' and now() - interval '7 days') as c30,
    (count(*) filter (where ts > now() - interval '7 days'))::float
      / nullif((count(*) filter (where ts between now() - interval '37 days' and now() - interval '7 days'))::float / (30.0 / 7), 0) as rising
  from events where type = 'search' and query is not null group by 1, 2 having count(*) >= 5;
create unique index mv_query_trends_key on mv_query_trends(lang, query);

-- +goose Down
drop materialized view mv_query_trends;
drop materialized view mv_daily_activity;
drop function playword_drop_old_event_partitions(int);
drop function playword_ensure_event_partitions(int);
drop table events;
