-- +goose Up

ALTER TABLE certificate_records
    ADD COLUMN IF NOT EXISTS completion_id UUID REFERENCES course_completions(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS student_display_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS course_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS course_title TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS result_summary TEXT NOT NULL DEFAULT 'completed',
    ADD COLUMN IF NOT EXISTS grade_summary TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_certificate_records_completion_id ON certificate_records(completion_id);

-- +goose Down

DROP INDEX IF EXISTS idx_certificate_records_completion_id;

ALTER TABLE certificate_records
    DROP COLUMN IF EXISTS grade_summary,
    DROP COLUMN IF EXISTS result_summary,
    DROP COLUMN IF EXISTS course_title,
    DROP COLUMN IF EXISTS course_code,
    DROP COLUMN IF EXISTS student_display_name,
    DROP COLUMN IF EXISTS completion_id;
