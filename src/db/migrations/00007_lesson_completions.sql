-- +goose Up

CREATE TABLE IF NOT EXISTS lesson_completions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lesson_id     UUID NOT NULL REFERENCES unit_lessons(id) ON DELETE CASCADE,
    student_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    completed_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT lesson_completions_lesson_student_unique UNIQUE (lesson_id, student_id)
);

CREATE INDEX IF NOT EXISTS idx_lesson_completions_lesson_id ON lesson_completions(lesson_id);
CREATE INDEX IF NOT EXISTS idx_lesson_completions_student_id ON lesson_completions(student_id);

-- +goose Down

DROP INDEX IF EXISTS idx_lesson_completions_student_id;
DROP INDEX IF EXISTS idx_lesson_completions_lesson_id;
DROP TABLE IF EXISTS lesson_completions;
