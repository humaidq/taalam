/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Course represents a course row.
type Course struct {
	ID              string
	Code            string
	Title           string
	Term            string
	CreatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	InstructorCount int
	StudentCount    int
}

// CourseUser represents an instructor or student attached to a course.
type CourseUser struct {
	ID          string
	DisplayName string
	Username    *string
	Role        UserRole
	CreatedAt   time.Time
	AssignedAt  time.Time
}

// CoursePersonOption represents a selectable user for course actions.
type CoursePersonOption struct {
	ID          string
	DisplayName string
	Username    *string
}

// CreateCourseInput defines fields for creating a course.
type CreateCourseInput struct {
	Code      string
	Title     string
	Term      string
	CreatedBy string
}

// UpdateCourseInput defines fields for updating a course.
type UpdateCourseInput struct {
	CourseID  string
	Code      string
	Title     string
	Term      string
	UpdatedBy string
}

// AssignCourseInstructorInput defines fields for assigning an instructor.
type AssignCourseInstructorInput struct {
	CourseID   string
	TeacherID  string
	AssignedBy string
}

// EnrollCourseStudentInput defines fields for enrolling a student.
type EnrollCourseStudentInput struct {
	CourseID   string
	StudentID  string
	EnrolledBy string
}

// ListCoursesForRole returns the courses visible to a user role.
func ListCoursesForRole(ctx context.Context, userID string, role UserRole) ([]Course, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	switch role {
	case RoleAdmin:
		return listCourses(ctx, `SELECT id, code, title, term, created_by, created_at, updated_at,
			(SELECT COUNT(*) FROM course_instructors ci WHERE ci.course_id = c.id),
			(SELECT COUNT(*) FROM course_enrollments ce WHERE ce.course_id = c.id)
		FROM courses c
		ORDER BY term DESC, code ASC, title ASC`)
	case RoleTeacher:
		return listCourses(ctx, `SELECT c.id, c.code, c.title, c.term, c.created_by, c.created_at, c.updated_at,
			(SELECT COUNT(*) FROM course_instructors ci2 WHERE ci2.course_id = c.id),
			(SELECT COUNT(*) FROM course_enrollments ce WHERE ce.course_id = c.id)
		FROM courses c
		JOIN course_instructors ci ON ci.course_id = c.id
		WHERE ci.teacher_id = $1
		ORDER BY c.term DESC, c.code ASC, c.title ASC`, userID)
	case RoleStudent:
		return listCourses(ctx, `SELECT c.id, c.code, c.title, c.term, c.created_by, c.created_at, c.updated_at,
			(SELECT COUNT(*) FROM course_instructors ci WHERE ci.course_id = c.id),
			(SELECT COUNT(*) FROM course_enrollments ce2 WHERE ce2.course_id = c.id)
		FROM courses c
		JOIN course_enrollments ce ON ce.course_id = c.id
		WHERE ce.student_id = $1
		ORDER BY c.term DESC, c.code ASC, c.title ASC`, userID)
	default:
		return nil, ErrInvalidRole
	}
}

// GetCourseByID returns a course by ID.
func GetCourseByID(ctx context.Context, courseID string) (*Course, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	var course Course
	err := pool.QueryRow(ctx, `
		SELECT c.id, c.code, c.title, c.term, c.created_by, c.created_at, c.updated_at,
			(SELECT COUNT(*) FROM course_instructors ci WHERE ci.course_id = c.id),
			(SELECT COUNT(*) FROM course_enrollments ce WHERE ce.course_id = c.id)
		FROM courses c
		WHERE c.id = $1
	`, courseID).Scan(
		&course.ID,
		&course.Code,
		&course.Title,
		&course.Term,
		&course.CreatedBy,
		&course.CreatedAt,
		&course.UpdatedAt,
		&course.InstructorCount,
		&course.StudentCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get course: %w", err)
	}

	return &course, nil
}

// ListCourseInstructors returns instructors for a course.
func ListCourseInstructors(ctx context.Context, courseID string) ([]CourseUser, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	return listCourseUsers(ctx, `
		SELECT u.id, u.display_name, u.username, u.role, u.created_at, ci.assigned_at
		FROM course_instructors ci
		JOIN users u ON u.id = ci.teacher_id
		WHERE ci.course_id = $1
		ORDER BY u.display_name ASC, u.created_at ASC
	`, courseID)
}

// ListCourseStudents returns enrolled students for a course.
func ListCourseStudents(ctx context.Context, courseID string) ([]CourseUser, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	return listCourseUsers(ctx, `
		SELECT u.id, u.display_name, u.username, u.role, u.created_at, ce.enrolled_at
		FROM course_enrollments ce
		JOIN users u ON u.id = ce.student_id
		WHERE ce.course_id = $1
		ORDER BY u.display_name ASC, u.created_at ASC
	`, courseID)
}

// ListAvailableTeachers returns teachers that are not yet assigned to the course.
func ListAvailableTeachers(ctx context.Context, courseID string) ([]CoursePersonOption, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	return listCoursePersonOptions(ctx, `
		SELECT u.id, u.display_name, u.username
		FROM users u
		WHERE u.role = $1
		  AND u.deactivated_at IS NULL
		  AND NOT EXISTS (
			SELECT 1
			FROM course_instructors ci
			WHERE ci.course_id = $2
			  AND ci.teacher_id = u.id
		  )
		ORDER BY u.display_name ASC, u.created_at ASC
	`, RoleTeacher, courseID)
}

// ListAvailableStudents returns students that are not yet enrolled in the course.
func ListAvailableStudents(ctx context.Context, courseID string) ([]CoursePersonOption, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	return listCoursePersonOptions(ctx, `
		SELECT u.id, u.display_name, u.username
		FROM users u
		WHERE u.role = $1
		  AND u.deactivated_at IS NULL
		  AND NOT EXISTS (
			SELECT 1
			FROM course_enrollments ce
			WHERE ce.course_id = $2
			  AND ce.student_id = u.id
		  )
		ORDER BY u.display_name ASC, u.created_at ASC
	`, RoleStudent, courseID)
}

// CreateCourse creates a course and appends a blockchain event.
func CreateCourse(ctx context.Context, input CreateCourseInput) (*Course, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	code := strings.ToUpper(strings.TrimSpace(input.Code))
	title := strings.TrimSpace(input.Title)
	term := strings.TrimSpace(input.Term)
	createdBy := strings.TrimSpace(input.CreatedBy)

	if code == "" {
		return nil, ErrCourseCodeRequired
	}
	if title == "" {
		return nil, ErrCourseTitleRequired
	}
	if term == "" {
		return nil, ErrCourseTermRequired
	}
	if _, err := uuid.Parse(createdBy); err != nil {
		return nil, ErrInvalidCreatorID
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin course transaction: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback course transaction", "error", rollbackErr)
		}
	}()

	if err := requireUserRoleTx(ctx, tx, createdBy, RoleAdmin); err != nil {
		return nil, err
	}

	if err := acquireBlockchainLockTx(ctx, tx); err != nil {
		return nil, err
	}

	var course Course
	err = tx.QueryRow(ctx, `
		INSERT INTO courses (code, title, term, created_by)
		VALUES ($1, $2, $3, $4)
		RETURNING id, code, title, term, created_by, created_at, updated_at
	`, code, title, term, createdBy).Scan(
		&course.ID,
		&course.Code,
		&course.Title,
		&course.Term,
		&course.CreatedBy,
		&course.CreatedAt,
		&course.UpdatedAt,
	)
	if err != nil {
		if isConstraintUniqueViolation(err, "courses_code_term_unique") {
			return nil, ErrCourseAlreadyExists
		}

		return nil, fmt.Errorf("failed to create course: %w", err)
	}

	if _, err := appendBlockchainEventTx(ctx, tx, buildCourseCreatedBlockchainEvent(course)); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit course transaction: %w", err)
	}

	return &course, nil
}

// UpdateCourse updates course metadata and appends a blockchain event.
func UpdateCourse(ctx context.Context, input UpdateCourseInput) (*Course, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	courseID := strings.TrimSpace(input.CourseID)
	code := strings.ToUpper(strings.TrimSpace(input.Code))
	title := strings.TrimSpace(input.Title)
	term := strings.TrimSpace(input.Term)
	updatedBy := strings.TrimSpace(input.UpdatedBy)

	if _, err := uuid.Parse(courseID); err != nil {
		return nil, ErrCourseNotFound
	}
	if code == "" {
		return nil, ErrCourseCodeRequired
	}
	if title == "" {
		return nil, ErrCourseTitleRequired
	}
	if term == "" {
		return nil, ErrCourseTermRequired
	}
	if _, err := uuid.Parse(updatedBy); err != nil {
		return nil, ErrInvalidCreatorID
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin course update transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback course update transaction", "error", rollbackErr)
		}
	}()

	if err := requireUserRoleTx(ctx, tx, updatedBy, RoleAdmin); err != nil {
		return nil, err
	}
	if err := acquireBlockchainLockTx(ctx, tx); err != nil {
		return nil, err
	}

	var course Course
	err = tx.QueryRow(ctx, `
		UPDATE courses
		SET code = $2,
		    title = $3,
		    term = $4,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING id, code, title, term, created_by, created_at, updated_at
	`, courseID, code, title, term).Scan(
		&course.ID,
		&course.Code,
		&course.Title,
		&course.Term,
		&course.CreatedBy,
		&course.CreatedAt,
		&course.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		if isConstraintUniqueViolation(err, "courses_code_term_unique") {
			return nil, ErrCourseAlreadyExists
		}

		return nil, fmt.Errorf("failed to update course: %w", err)
	}

	if err := hydrateCourseCountsTx(ctx, tx, &course); err != nil {
		return nil, err
	}
	if _, err := appendBlockchainEventTx(ctx, tx, buildCourseUpdatedBlockchainEvent(course, updatedBy)); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit course update transaction: %w", err)
	}

	return &course, nil
}

// DeleteCourse removes a course and its cascading content.
func DeleteCourse(ctx context.Context, courseID string, deletedBy string) error {
	if pool == nil {
		return ErrDatabaseConnectionNotInitialized
	}

	courseID = strings.TrimSpace(courseID)
	deletedBy = strings.TrimSpace(deletedBy)
	if _, err := uuid.Parse(courseID); err != nil {
		return ErrCourseNotFound
	}
	if _, err := uuid.Parse(deletedBy); err != nil {
		return ErrInvalidCreatorID
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin course delete transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback course delete transaction", "error", rollbackErr)
		}
	}()

	if err := requireUserRoleTx(ctx, tx, deletedBy, RoleAdmin); err != nil {
		return err
	}
	if err := acquireBlockchainLockTx(ctx, tx); err != nil {
		return err
	}

	course, err := getCourseByIDTx(ctx, tx, courseID)
	if err != nil {
		return err
	}
	if _, err := appendBlockchainEventTx(ctx, tx, buildCourseDeletedBlockchainEvent(*course, deletedBy)); err != nil {
		return err
	}

	command, err := tx.Exec(ctx, `DELETE FROM courses WHERE id = $1`, courseID)
	if err != nil {
		return fmt.Errorf("failed to delete course: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrCourseNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit course delete transaction: %w", err)
	}

	return nil
}

// AssignCourseInstructor assigns a teacher to a course and appends a blockchain event.
func AssignCourseInstructor(ctx context.Context, input AssignCourseInstructorInput) error {
	if pool == nil {
		return ErrDatabaseConnectionNotInitialized
	}

	courseID := strings.TrimSpace(input.CourseID)
	teacherID := strings.TrimSpace(input.TeacherID)
	assignedBy := strings.TrimSpace(input.AssignedBy)
	if _, err := uuid.Parse(courseID); err != nil {
		return ErrCourseNotFound
	}
	if _, err := uuid.Parse(teacherID); err != nil {
		return ErrUserNotFound
	}
	if _, err := uuid.Parse(assignedBy); err != nil {
		return ErrInvalidCreatorID
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin course instructor transaction: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback course instructor transaction", "error", rollbackErr)
		}
	}()

	if err := requireUserRoleTx(ctx, tx, assignedBy, RoleAdmin); err != nil {
		return err
	}
	if err := requireUserRoleTx(ctx, tx, teacherID, RoleTeacher); err != nil {
		return err
	}
	if err := ensureCourseExistsTx(ctx, tx, courseID); err != nil {
		return err
	}
	if err := acquireBlockchainLockTx(ctx, tx); err != nil {
		return err
	}

	command, err := tx.Exec(ctx, `
		INSERT INTO course_instructors (course_id, teacher_id, assigned_by)
		VALUES ($1, $2, $3)
		ON CONFLICT (course_id, teacher_id) DO NOTHING
	`, courseID, teacherID, assignedBy)
	if err != nil {
		return fmt.Errorf("failed to assign course instructor: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrCourseInstructorAlreadyAssigned
	}

	if _, err := appendBlockchainEventTx(ctx, tx, buildCourseInstructorAssignedBlockchainEvent(courseID, teacherID, assignedBy)); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit course instructor transaction: %w", err)
	}

	return nil
}

// EnrollCourseStudent enrolls a student in a course and appends a blockchain event.
func EnrollCourseStudent(ctx context.Context, input EnrollCourseStudentInput) error {
	if pool == nil {
		return ErrDatabaseConnectionNotInitialized
	}

	courseID := strings.TrimSpace(input.CourseID)
	studentID := strings.TrimSpace(input.StudentID)
	enrolledBy := strings.TrimSpace(input.EnrolledBy)
	if _, err := uuid.Parse(courseID); err != nil {
		return ErrCourseNotFound
	}
	if _, err := uuid.Parse(studentID); err != nil {
		return ErrUserNotFound
	}
	if _, err := uuid.Parse(enrolledBy); err != nil {
		return ErrInvalidCreatorID
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin course enrollment transaction: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback course enrollment transaction", "error", rollbackErr)
		}
	}()

	if err := requireUserRoleTx(ctx, tx, enrolledBy, RoleAdmin); err != nil {
		return err
	}
	if err := requireUserRoleTx(ctx, tx, studentID, RoleStudent); err != nil {
		return err
	}
	if err := ensureCourseExistsTx(ctx, tx, courseID); err != nil {
		return err
	}
	if err := acquireBlockchainLockTx(ctx, tx); err != nil {
		return err
	}

	command, err := tx.Exec(ctx, `
		INSERT INTO course_enrollments (course_id, student_id, enrolled_by)
		VALUES ($1, $2, $3)
		ON CONFLICT (course_id, student_id) DO NOTHING
	`, courseID, studentID, enrolledBy)
	if err != nil {
		return fmt.Errorf("failed to enroll course student: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrCourseStudentAlreadyEnrolled
	}

	if _, err := appendBlockchainEventTx(ctx, tx, buildCourseStudentEnrolledBlockchainEvent(courseID, studentID, enrolledBy)); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit course enrollment transaction: %w", err)
	}

	return nil
}

// IsUserCourseInstructor reports whether the given teacher is assigned to the course.
func IsUserCourseInstructor(ctx context.Context, userID string, courseID string) (bool, error) {
	if pool == nil {
		return false, ErrDatabaseConnectionNotInitialized
	}

	var allowed bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM course_instructors ci
			JOIN users u ON u.id = ci.teacher_id
			WHERE ci.course_id = $1
			  AND ci.teacher_id = $2
			  AND u.deactivated_at IS NULL
			  AND u.role = $3
		)
	`, courseID, userID, RoleTeacher).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("failed to check course instructor access: %w", err)
	}

	return allowed, nil
}

// IsUserCourseStudent reports whether the given student is enrolled in the course.
func IsUserCourseStudent(ctx context.Context, userID string, courseID string) (bool, error) {
	if pool == nil {
		return false, ErrDatabaseConnectionNotInitialized
	}

	var allowed bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM course_enrollments ce
			JOIN users u ON u.id = ce.student_id
			WHERE ce.course_id = $1
			  AND ce.student_id = $2
			  AND u.deactivated_at IS NULL
			  AND u.role = $3
		)
	`, courseID, userID, RoleStudent).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("failed to check course student access: %w", err)
	}

	return allowed, nil
}

// CanUserViewCourse reports whether a user can view a course.
func CanUserViewCourse(ctx context.Context, userID string, courseID string) (bool, error) {
	if pool == nil {
		return false, ErrDatabaseConnectionNotInitialized
	}

	var allowed bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM users u
			WHERE u.id = $1
			  AND u.deactivated_at IS NULL
			  AND (
				u.role = 'admin'
				OR (
					u.role = 'teacher'
					AND EXISTS (
						SELECT 1
						FROM course_instructors ci
						WHERE ci.course_id = $2
						  AND ci.teacher_id = u.id
					)
				)
				OR (
					u.role = 'student'
					AND EXISTS (
						SELECT 1
						FROM course_enrollments ce
						WHERE ce.course_id = $2
						  AND ce.student_id = u.id
					)
				)
			  )
		)
	`, userID, courseID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("failed to check course view access: %w", err)
	}

	return allowed, nil
}

// CanUserManageCourse reports whether a user can manage a course.
func CanUserManageCourse(ctx context.Context, userID string, courseID string) (bool, error) {
	if pool == nil {
		return false, ErrDatabaseConnectionNotInitialized
	}

	var allowed bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM users u
			WHERE u.id = $1
			  AND u.deactivated_at IS NULL
			  AND (
				u.role = 'admin'
				OR (
					u.role = 'teacher'
					AND EXISTS (
						SELECT 1
						FROM course_instructors ci
						WHERE ci.course_id = $2
						  AND ci.teacher_id = u.id
					)
				)
			  )
		)
	`, userID, courseID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("failed to check course management access: %w", err)
	}

	return allowed, nil
}

// CanUserViewSubmission reports whether a user can view a submission.
func CanUserViewSubmission(ctx context.Context, userID string, submissionID string) (bool, error) {
	if pool == nil {
		return false, ErrDatabaseConnectionNotInitialized
	}

	var allowed bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM users u
			JOIN submissions s ON s.id = $2
			JOIN assignments a ON a.id = s.assignment_id
			WHERE u.id = $1
			  AND u.deactivated_at IS NULL
			  AND (
				u.role = 'admin'
				OR (u.role = 'student' AND s.student_id = u.id)
				OR (
					u.role = 'teacher'
					AND EXISTS (
						SELECT 1
						FROM course_instructors ci
						WHERE ci.course_id = a.course_id
						  AND ci.teacher_id = u.id
					)
				)
			  )
		)
	`, userID, submissionID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("failed to check submission view access: %w", err)
	}

	return allowed, nil
}

// CanUserViewGrade reports whether a user can view a grade.
func CanUserViewGrade(ctx context.Context, userID string, gradeID string) (bool, error) {
	if pool == nil {
		return false, ErrDatabaseConnectionNotInitialized
	}

	var allowed bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM users u
			JOIN grades g ON g.id = $2
			JOIN assignments a ON a.id = g.assignment_id
			WHERE u.id = $1
			  AND u.deactivated_at IS NULL
			  AND (
				u.role = 'admin'
				OR (u.role = 'student' AND g.student_id = u.id)
				OR (
					u.role = 'teacher'
					AND EXISTS (
						SELECT 1
						FROM course_instructors ci
						WHERE ci.course_id = a.course_id
						  AND ci.teacher_id = u.id
					)
				)
			  )
		)
	`, userID, gradeID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("failed to check grade view access: %w", err)
	}

	return allowed, nil
}

type courseCreatedEventData struct {
	CourseID  string `json:"course_id"`
	Code      string `json:"code"`
	Title     string `json:"title"`
	Term      string `json:"term"`
	CreatedBy string `json:"created_by"`
}

type courseUpdatedEventData struct {
	CourseID  string `json:"course_id"`
	Code      string `json:"code"`
	Title     string `json:"title"`
	Term      string `json:"term"`
	UpdatedBy string `json:"updated_by"`
}

type courseDeletedEventData struct {
	CourseID  string `json:"course_id"`
	Code      string `json:"code"`
	Title     string `json:"title"`
	Term      string `json:"term"`
	DeletedBy string `json:"deleted_by"`
}

type courseInstructorAssignedEventData struct {
	CourseID   string `json:"course_id"`
	TeacherID  string `json:"teacher_id"`
	AssignedBy string `json:"assigned_by"`
}

type courseStudentEnrolledEventData struct {
	CourseID   string `json:"course_id"`
	StudentID  string `json:"student_id"`
	EnrolledBy string `json:"enrolled_by"`
}

func buildCourseCreatedBlockchainEvent(course Course) AppendBlockchainEventInput {
	return AppendBlockchainEventInput{
		EventType:   "course_created",
		EntityType:  "course",
		EntityID:    course.ID,
		ActorUserID: course.CreatedBy,
		OccurredAt:  course.CreatedAt,
		Data: courseCreatedEventData{
			CourseID:  course.ID,
			Code:      course.Code,
			Title:     course.Title,
			Term:      course.Term,
			CreatedBy: course.CreatedBy,
		},
	}
}

func buildCourseUpdatedBlockchainEvent(course Course, updatedBy string) AppendBlockchainEventInput {
	return AppendBlockchainEventInput{
		EventType:   "course_updated",
		EntityType:  "course",
		EntityID:    course.ID,
		ActorUserID: updatedBy,
		OccurredAt:  course.UpdatedAt,
		Data: courseUpdatedEventData{
			CourseID:  course.ID,
			Code:      course.Code,
			Title:     course.Title,
			Term:      course.Term,
			UpdatedBy: updatedBy,
		},
	}
}

func buildCourseDeletedBlockchainEvent(course Course, deletedBy string) AppendBlockchainEventInput {
	return AppendBlockchainEventInput{
		EventType:   "course_deleted",
		EntityType:  "course",
		EntityID:    course.ID,
		ActorUserID: deletedBy,
		OccurredAt:  time.Now().UTC(),
		Data: courseDeletedEventData{
			CourseID:  course.ID,
			Code:      course.Code,
			Title:     course.Title,
			Term:      course.Term,
			DeletedBy: deletedBy,
		},
	}
}

func buildCourseInstructorAssignedBlockchainEvent(courseID string, teacherID string, assignedBy string) AppendBlockchainEventInput {
	return AppendBlockchainEventInput{
		EventType:   "instructor_assigned",
		EntityType:  "course",
		EntityID:    courseID,
		ActorUserID: assignedBy,
		OccurredAt:  time.Now().UTC(),
		Data: courseInstructorAssignedEventData{
			CourseID:   courseID,
			TeacherID:  teacherID,
			AssignedBy: assignedBy,
		},
	}
}

func buildCourseStudentEnrolledBlockchainEvent(courseID string, studentID string, enrolledBy string) AppendBlockchainEventInput {
	return AppendBlockchainEventInput{
		EventType:   "student_enrolled",
		EntityType:  "course",
		EntityID:    courseID,
		ActorUserID: enrolledBy,
		OccurredAt:  time.Now().UTC(),
		Data: courseStudentEnrolledEventData{
			CourseID:   courseID,
			StudentID:  studentID,
			EnrolledBy: enrolledBy,
		},
	}
}

func getCourseByIDTx(ctx context.Context, tx pgx.Tx, courseID string) (*Course, error) {
	var course Course
	err := tx.QueryRow(ctx, `
		SELECT c.id, c.code, c.title, c.term, c.created_by, c.created_at, c.updated_at,
			(SELECT COUNT(*) FROM course_instructors ci WHERE ci.course_id = c.id),
			(SELECT COUNT(*) FROM course_enrollments ce WHERE ce.course_id = c.id)
		FROM courses c
		WHERE c.id = $1
	`, courseID).Scan(
		&course.ID,
		&course.Code,
		&course.Title,
		&course.Term,
		&course.CreatedBy,
		&course.CreatedAt,
		&course.UpdatedAt,
		&course.InstructorCount,
		&course.StudentCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load course: %w", err)
	}

	return &course, nil
}

func hydrateCourseCountsTx(ctx context.Context, tx pgx.Tx, course *Course) error {
	err := tx.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM course_instructors WHERE course_id = $1),
			(SELECT COUNT(*) FROM course_enrollments WHERE course_id = $1)
	`, course.ID).Scan(&course.InstructorCount, &course.StudentCount)
	if err != nil {
		return fmt.Errorf("failed to load course counts: %w", err)
	}

	return nil
}

func listCourses(ctx context.Context, query string, args ...any) ([]Course, error) {
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list courses: %w", err)
	}
	defer rows.Close()

	courses := make([]Course, 0)
	for rows.Next() {
		var course Course
		if err := rows.Scan(
			&course.ID,
			&course.Code,
			&course.Title,
			&course.Term,
			&course.CreatedBy,
			&course.CreatedAt,
			&course.UpdatedAt,
			&course.InstructorCount,
			&course.StudentCount,
		); err != nil {
			return nil, fmt.Errorf("failed to scan course: %w", err)
		}

		courses = append(courses, course)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating courses: %w", err)
	}

	return courses, nil
}

func listCourseUsers(ctx context.Context, query string, args ...any) ([]CourseUser, error) {
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list course users: %w", err)
	}
	defer rows.Close()

	users := make([]CourseUser, 0)
	for rows.Next() {
		var user CourseUser
		var rawRole string
		if err := rows.Scan(&user.ID, &user.DisplayName, &user.Username, &rawRole, &user.CreatedAt, &user.AssignedAt); err != nil {
			return nil, fmt.Errorf("failed to scan course user: %w", err)
		}

		role, err := NormalizeRole(rawRole)
		if err != nil {
			return nil, err
		}
		user.Role = role
		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating course users: %w", err)
	}

	return users, nil
}

func listCoursePersonOptions(ctx context.Context, query string, args ...any) ([]CoursePersonOption, error) {
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list course person options: %w", err)
	}
	defer rows.Close()

	options := make([]CoursePersonOption, 0)
	for rows.Next() {
		var option CoursePersonOption
		if err := rows.Scan(&option.ID, &option.DisplayName, &option.Username); err != nil {
			return nil, fmt.Errorf("failed to scan course person option: %w", err)
		}

		options = append(options, option)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating course person options: %w", err)
	}

	return options, nil
}

func ensureCourseExistsTx(ctx context.Context, tx pgx.Tx, courseID string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM courses WHERE id = $1)`, courseID).Scan(&exists); err != nil {
		return fmt.Errorf("failed to check course existence: %w", err)
	}
	if !exists {
		return ErrCourseNotFound
	}

	return nil
}

func requireUserRoleTx(ctx context.Context, tx pgx.Tx, userID string, required UserRole) error {
	var rawRole string
	err := tx.QueryRow(ctx, `SELECT role FROM users WHERE id = $1 AND deactivated_at IS NULL`, userID).Scan(&rawRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to load user role: %w", err)
	}

	role, err := NormalizeRole(rawRole)
	if err != nil {
		return err
	}
	if role != required {
		switch required {
		case RoleAdmin:
			return ErrAdminRequired
		case RoleTeacher:
			return ErrTeacherRequired
		case RoleStudent:
			return ErrStudentRequired
		default:
			return ErrAccessDenied
		}
	}

	return nil
}

func isConstraintUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" && pgErr.ConstraintName == constraint
	}

	return false
}
