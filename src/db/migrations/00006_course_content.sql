-- +goose Up

CREATE TABLE IF NOT EXISTS course_units (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    course_id   UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_by  UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_course_units_course_id ON course_units(course_id);
CREATE INDEX IF NOT EXISTS idx_course_units_created_by ON course_units(created_by);

DROP TRIGGER IF EXISTS course_units_updated_at ON course_units;
CREATE TRIGGER course_units_updated_at
    BEFORE UPDATE ON course_units
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TABLE IF NOT EXISTS unit_lessons (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    unit_id     UUID NOT NULL REFERENCES course_units(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    position    INTEGER NOT NULL,
    created_by  UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT unit_lessons_position_positive CHECK (position >= 1),
    CONSTRAINT unit_lessons_unit_position_unique UNIQUE (unit_id, position)
);

CREATE INDEX IF NOT EXISTS idx_unit_lessons_unit_id ON unit_lessons(unit_id);
CREATE INDEX IF NOT EXISTS idx_unit_lessons_created_by ON unit_lessons(created_by);

DROP TRIGGER IF EXISTS unit_lessons_updated_at ON unit_lessons;
CREATE TRIGGER unit_lessons_updated_at
    BEFORE UPDATE ON unit_lessons
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TABLE IF NOT EXISTS lesson_slides (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lesson_id     UUID NOT NULL REFERENCES unit_lessons(id) ON DELETE CASCADE,
    title         TEXT NOT NULL DEFAULT '',
    markdown_raw  TEXT NOT NULL,
    position      INTEGER NOT NULL,
    created_by    UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT lesson_slides_position_positive CHECK (position >= 1),
    CONSTRAINT lesson_slides_lesson_position_unique UNIQUE (lesson_id, position)
);

CREATE INDEX IF NOT EXISTS idx_lesson_slides_lesson_id ON lesson_slides(lesson_id);
CREATE INDEX IF NOT EXISTS idx_lesson_slides_created_by ON lesson_slides(created_by);

DROP TRIGGER IF EXISTS lesson_slides_updated_at ON lesson_slides;
CREATE TRIGGER lesson_slides_updated_at
    BEFORE UPDATE ON lesson_slides
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TABLE IF NOT EXISTS course_outline_items (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    course_id      UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    position       INTEGER NOT NULL,
    item_type      TEXT NOT NULL,
    unit_id        UUID REFERENCES course_units(id) ON DELETE CASCADE,
    assignment_id  UUID REFERENCES assignments(id) ON DELETE CASCADE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT course_outline_items_position_positive CHECK (position >= 1),
    CONSTRAINT course_outline_items_type_check CHECK (item_type IN ('unit', 'assignment')),
    CONSTRAINT course_outline_items_position_unique UNIQUE (course_id, position),
    CONSTRAINT course_outline_items_target_check CHECK (
        (item_type = 'unit' AND unit_id IS NOT NULL AND assignment_id IS NULL)
        OR (item_type = 'assignment' AND assignment_id IS NOT NULL AND unit_id IS NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_course_outline_items_course_id ON course_outline_items(course_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_course_outline_items_unit_id_unique ON course_outline_items(unit_id) WHERE unit_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_course_outline_items_assignment_id_unique ON course_outline_items(assignment_id) WHERE assignment_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS idx_course_outline_items_assignment_id_unique;
DROP INDEX IF EXISTS idx_course_outline_items_unit_id_unique;
DROP INDEX IF EXISTS idx_course_outline_items_course_id;
DROP TABLE IF EXISTS course_outline_items;

DROP TRIGGER IF EXISTS lesson_slides_updated_at ON lesson_slides;
DROP INDEX IF EXISTS idx_lesson_slides_created_by;
DROP INDEX IF EXISTS idx_lesson_slides_lesson_id;
DROP TABLE IF EXISTS lesson_slides;

DROP TRIGGER IF EXISTS unit_lessons_updated_at ON unit_lessons;
DROP INDEX IF EXISTS idx_unit_lessons_created_by;
DROP INDEX IF EXISTS idx_unit_lessons_unit_id;
DROP TABLE IF EXISTS unit_lessons;

DROP TRIGGER IF EXISTS course_units_updated_at ON course_units;
DROP INDEX IF EXISTS idx_course_units_created_by;
DROP INDEX IF EXISTS idx_course_units_course_id;
DROP TABLE IF EXISTS course_units;
