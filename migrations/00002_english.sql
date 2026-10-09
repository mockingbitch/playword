-- +goose Up
-- English (M4): pronunciations come from CMUdict or are derived from a stem
-- (spec §1.6); syllables.phonemes holds an English entry's rhyme segments.
alter table entries add column phonetic_confidence real not null default 1;

-- +goose Down
alter table entries drop column phonetic_confidence;
