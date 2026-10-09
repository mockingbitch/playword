-- +goose Up
-- When periodic jobs last ran, so a weekly job is not repeated by restarts.
create table job_runs (
  name     text primary key,
  last_run timestamptz not null
);

-- +goose Down
drop table job_runs;
