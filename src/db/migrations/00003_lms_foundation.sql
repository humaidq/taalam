-- +goose Up

CREATE TABLE IF NOT EXISTS courses (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code       TEXT NOT NULL,
    title      TEXT NOT NULL,
    term       TEXT NOT NULL,
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT courses_code_term_unique UNIQUE (code, term)
);

CREATE INDEX IF NOT EXISTS idx_courses_created_by ON courses(created_by);
CREATE INDEX IF NOT EXISTS idx_courses_term ON courses(term);

DROP TRIGGER IF EXISTS courses_updated_at ON courses;
CREATE TRIGGER courses_updated_at
    BEFORE UPDATE ON courses
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TABLE IF NOT EXISTS course_instructors (
    course_id    UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    teacher_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    assigned_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    assigned_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (course_id, teacher_id)
);

CREATE INDEX IF NOT EXISTS idx_course_instructors_teacher_id ON course_instructors(teacher_id);

CREATE TABLE IF NOT EXISTS course_enrollments (
    course_id    UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    student_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    enrolled_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    enrolled_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (course_id, student_id)
);

CREATE INDEX IF NOT EXISTS idx_course_enrollments_student_id ON course_enrollments(student_id);

CREATE TABLE IF NOT EXISTS assignments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    course_id   UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    due_at      TIMESTAMPTZ,
    max_grade   NUMERIC(6,2) NOT NULL,
    created_by  UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT assignments_max_grade_nonnegative CHECK (max_grade >= 0)
);

CREATE INDEX IF NOT EXISTS idx_assignments_course_id ON assignments(course_id);
CREATE INDEX IF NOT EXISTS idx_assignments_course_due_at ON assignments(course_id, due_at);
CREATE INDEX IF NOT EXISTS idx_assignments_created_by ON assignments(created_by);

DROP TRIGGER IF EXISTS assignments_updated_at ON assignments;
CREATE TRIGGER assignments_updated_at
    BEFORE UPDATE ON assignments
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TABLE IF NOT EXISTS submissions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    assignment_id UUID NOT NULL REFERENCES assignments(id) ON DELETE CASCADE,
    student_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    version      INTEGER NOT NULL,
    file_name    TEXT NOT NULL,
    content_type TEXT,
    file_size    BIGINT NOT NULL,
    file_sha256  TEXT NOT NULL,
    file_bytes   BYTEA NOT NULL,
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT submissions_version_positive CHECK (version >= 1),
    CONSTRAINT submissions_file_size_nonnegative CHECK (file_size >= 0),
    CONSTRAINT submissions_assignment_student_version_unique UNIQUE (assignment_id, student_id, version),
    CONSTRAINT submissions_file_sha256_hex CHECK (file_sha256 ~ '^[0-9a-f]{64}$')
);

CREATE INDEX IF NOT EXISTS idx_submissions_assignment_student_version ON submissions(assignment_id, student_id, version DESC);
CREATE INDEX IF NOT EXISTS idx_submissions_student_id ON submissions(student_id);
CREATE INDEX IF NOT EXISTS idx_submissions_submitted_at ON submissions(submitted_at DESC);

CREATE TABLE IF NOT EXISTS grades (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    assignment_id   UUID NOT NULL REFERENCES assignments(id) ON DELETE CASCADE,
    student_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    submission_id   UUID NOT NULL REFERENCES submissions(id) ON DELETE CASCADE,
    version         INTEGER NOT NULL,
    grade_value     NUMERIC(6,2) NOT NULL,
    feedback_text   TEXT NOT NULL DEFAULT '',
    commitment_hash TEXT NOT NULL,
    published_by    UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    published_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    previous_grade_id UUID REFERENCES grades(id) ON DELETE SET NULL,
    CONSTRAINT grades_version_positive CHECK (version >= 1),
    CONSTRAINT grades_grade_value_nonnegative CHECK (grade_value >= 0),
    CONSTRAINT grades_assignment_student_version_unique UNIQUE (assignment_id, student_id, version),
    CONSTRAINT grades_commitment_hash_hex CHECK (commitment_hash ~ '^[0-9a-f]{64}$')
);

CREATE INDEX IF NOT EXISTS idx_grades_assignment_student_version ON grades(assignment_id, student_id, version DESC);
CREATE INDEX IF NOT EXISTS idx_grades_submission_id ON grades(submission_id);
CREATE INDEX IF NOT EXISTS idx_grades_published_by ON grades(published_by);
CREATE INDEX IF NOT EXISTS idx_grades_published_at ON grades(published_at DESC);

CREATE TABLE IF NOT EXISTS course_completions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    course_id    UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    student_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    marked_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    completed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status       TEXT NOT NULL DEFAULT 'completed',
    CONSTRAINT course_completions_status_check CHECK (status IN ('completed', 'revoked')),
    CONSTRAINT course_completions_course_student_unique UNIQUE (course_id, student_id)
);

CREATE INDEX IF NOT EXISTS idx_course_completions_student_id ON course_completions(student_id);
CREATE INDEX IF NOT EXISTS idx_course_completions_status ON course_completions(status);

CREATE TABLE IF NOT EXISTS certificate_records (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    course_id        UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    student_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    certificate_code TEXT NOT NULL,
    certificate_hash TEXT NOT NULL,
    issued_by        UUID REFERENCES users(id) ON DELETE SET NULL,
    issued_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at       TIMESTAMPTZ,
    revoked_by       UUID REFERENCES users(id) ON DELETE SET NULL,
    CONSTRAINT certificate_records_code_unique UNIQUE (certificate_code),
    CONSTRAINT certificate_records_hash_hex CHECK (certificate_hash ~ '^[0-9a-f]{64}$')
);

CREATE INDEX IF NOT EXISTS idx_certificate_records_student_id ON certificate_records(student_id);
CREATE INDEX IF NOT EXISTS idx_certificate_records_course_id ON certificate_records(course_id);
CREATE INDEX IF NOT EXISTS idx_certificate_records_issued_at ON certificate_records(issued_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_certificate_records_active_course_student
    ON certificate_records(course_id, student_id)
    WHERE revoked_at IS NULL;

-- +goose Down

DROP INDEX IF EXISTS idx_certificate_records_active_course_student;
DROP INDEX IF EXISTS idx_certificate_records_issued_at;
DROP INDEX IF EXISTS idx_certificate_records_course_id;
DROP INDEX IF EXISTS idx_certificate_records_student_id;
DROP TABLE IF EXISTS certificate_records;

DROP INDEX IF EXISTS idx_course_completions_status;
DROP INDEX IF EXISTS idx_course_completions_student_id;
DROP TABLE IF EXISTS course_completions;

DROP INDEX IF EXISTS idx_grades_published_at;
DROP INDEX IF EXISTS idx_grades_published_by;
DROP INDEX IF EXISTS idx_grades_submission_id;
DROP INDEX IF EXISTS idx_grades_assignment_student_version;
DROP TABLE IF EXISTS grades;

DROP INDEX IF EXISTS idx_submissions_submitted_at;
DROP INDEX IF EXISTS idx_submissions_student_id;
DROP INDEX IF EXISTS idx_submissions_assignment_student_version;
DROP TABLE IF EXISTS submissions;

DROP TRIGGER IF EXISTS assignments_updated_at ON assignments;
DROP INDEX IF EXISTS idx_assignments_created_by;
DROP INDEX IF EXISTS idx_assignments_course_due_at;
DROP INDEX IF EXISTS idx_assignments_course_id;
DROP TABLE IF EXISTS assignments;

DROP INDEX IF EXISTS idx_course_enrollments_student_id;
DROP TABLE IF EXISTS course_enrollments;

DROP INDEX IF EXISTS idx_course_instructors_teacher_id;
DROP TABLE IF EXISTS course_instructors;

DROP TRIGGER IF EXISTS courses_updated_at ON courses;
DROP INDEX IF EXISTS idx_courses_term;
DROP INDEX IF EXISTS idx_courses_created_by;
DROP TABLE IF EXISTS courses;
