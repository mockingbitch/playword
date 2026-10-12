-- +goose Up
-- The writing desk: each rhyme book holds the lyrics it collects rhymes for.
alter table rhyme_books add column lyrics text not null default '';

-- +goose Down
alter table rhyme_books drop column lyrics;
