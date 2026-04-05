/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package db

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Assignment represents an assignment row plus derived display fields.
type Assignment struct {
	ID           string
	CourseID     string
	Title        string
	Description  string
	DueAt        *time.Time
	MaxGrade     string
	CreatedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	MetadataHash string
}

// CreateAssignmentInput defines fields for creating an assignment.
type CreateAssignmentInput struct {
	CourseID          string
	Title             string
	Description       string
	DueAt             *time.Time
	MaxGrade          string
	InsertAfterItemID string
	CreatedBy         string
}

// UpdateAssignmentInput defines fields for updating an assignment.
type UpdateAssignmentInput struct {
	AssignmentID string
	CourseID     string
	Title        string
	Description  string
	DueAt        *time.Time
	MaxGrade     string
	UpdatedBy    string
}

// ListAssignmentsForCourse returns assignments for a course ordered by due date.
func ListAssignmentsForCourse(ctx context.Context, courseID string) ([]Assignment, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	rows, err := pool.Query(ctx, `
		SELECT id, course_id, title, description, due_at, max_grade::text, created_by, created_at, updated_at
		FROM assignments
		WHERE course_id = $1
		ORDER BY due_at ASC NULLS LAST, created_at DESC
	`, courseID)
	if err != nil {
		return nil, fmt.Errorf("failed to list assignments: %w", err)
	}
	defer rows.Close()

	assignments := make([]Assignment, 0)
	for rows.Next() {
		assignment, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}

		assignments = append(assignments, assignment)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating assignments: %w", err)
	}

	return assignments, nil
}

// GetAssignmentByID returns a single assignment.
func GetAssignmentByID(ctx context.Context, assignmentID string) (*Assignment, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	row := pool.QueryRow(ctx, `
		SELECT id, course_id, title, description, due_at, max_grade::text, created_by, created_at, updated_at
		FROM assignments
		WHERE id = $1
	`, assignmentID)

	assignment, err := scanAssignment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAssignmentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get assignment: %w", err)
	}

	return &assignment, nil
}

// CreateAssignment creates a course assignment and emits an audit event.
func CreateAssignment(ctx context.Context, input CreateAssignmentInput) (*Assignment, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	courseID := strings.TrimSpace(input.CourseID)
	title := strings.TrimSpace(input.Title)
	description := strings.TrimSpace(input.Description)
	createdBy := strings.TrimSpace(input.CreatedBy)
	insertAfterItemID := strings.TrimSpace(input.InsertAfterItemID)
	maxGradeValue, maxGradeDisplay, err := normalizeAssignmentMaxGrade(input.MaxGrade)
	if err != nil {
		return nil, err
	}

	if _, err := uuid.Parse(courseID); err != nil {
		return nil, ErrCourseNotFound
	}
	if title == "" {
		return nil, ErrAssignmentTitleRequired
	}
	if _, err := uuid.Parse(createdBy); err != nil {
		return nil, ErrInvalidCreatorID
	}

	var dueAt *time.Time
	if input.DueAt != nil {
		timeValue := input.DueAt.UTC()
		dueAt = &timeValue
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin assignment transaction: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback assignment transaction", "error", rollbackErr)
		}
	}()

	if err := ensureCourseExistsTx(ctx, tx, courseID); err != nil {
		return nil, err
	}
	if err := requireCourseManagementAccessTx(ctx, tx, createdBy, courseID); err != nil {
		return nil, err
	}
	if err := acquireBlockchainLockTx(ctx, tx); err != nil {
		return nil, err
	}

	var assignment Assignment
	err = tx.QueryRow(ctx, `
		INSERT INTO assignments (course_id, title, description, due_at, max_grade, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, course_id, title, description, due_at, max_grade::text, created_by, created_at, updated_at
	`, courseID, title, description, dueAt, maxGradeValue, createdBy).Scan(
		&assignment.ID,
		&assignment.CourseID,
		&assignment.Title,
		&assignment.Description,
		&assignment.DueAt,
		&assignment.MaxGrade,
		&assignment.CreatedBy,
		&assignment.CreatedAt,
		&assignment.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create assignment: %w", err)
	}

	assignment.MaxGrade = maxGradeDisplay
	assignment.MetadataHash, err = computeAssignmentMetadataHash(assignment)
	if err != nil {
		return nil, err
	}

	if err := AddAssignmentToCourseOutlineTx(ctx, tx, courseID, assignment.ID, insertAfterItemID); err != nil {
		return nil, err
	}

	if _, err := appendBlockchainEventTx(ctx, tx, buildAssignmentPublishedBlockchainEvent(assignment)); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit assignment transaction: %w", err)
	}

	return &assignment, nil
}

// UpdateAssignment updates assignment metadata and appends a blockchain event.
func UpdateAssignment(ctx context.Context, input UpdateAssignmentInput) (*Assignment, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	assignmentID := strings.TrimSpace(input.AssignmentID)
	courseID := strings.TrimSpace(input.CourseID)
	title := strings.TrimSpace(input.Title)
	description := strings.TrimSpace(input.Description)
	updatedBy := strings.TrimSpace(input.UpdatedBy)
	maxGradeValue, maxGradeDisplay, err := normalizeAssignmentMaxGrade(input.MaxGrade)
	if err != nil {
		return nil, err
	}

	if _, err := uuid.Parse(assignmentID); err != nil {
		return nil, ErrAssignmentNotFound
	}
	if _, err := uuid.Parse(courseID); err != nil {
		return nil, ErrCourseNotFound
	}
	if title == "" {
		return nil, ErrAssignmentTitleRequired
	}
	if _, err := uuid.Parse(updatedBy); err != nil {
		return nil, ErrInvalidCreatorID
	}

	var dueAt *time.Time
	if input.DueAt != nil {
		timeValue := input.DueAt.UTC()
		dueAt = &timeValue
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin assignment update transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback assignment update transaction", "error", rollbackErr)
		}
	}()

	if err := requireCourseManagementAccessTx(ctx, tx, updatedBy, courseID); err != nil {
		return nil, err
	}
	if err := acquireBlockchainLockTx(ctx, tx); err != nil {
		return nil, err
	}

	resolvedCourseID, err := getAssignmentCourseIDTx(ctx, tx, assignmentID)
	if err != nil {
		return nil, err
	}
	if resolvedCourseID != courseID {
		return nil, ErrAssignmentNotFound
	}

	var assignment Assignment
	err = tx.QueryRow(ctx, `
		UPDATE assignments
		SET title = $2,
		    description = $3,
		    due_at = $4,
		    max_grade = $5,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING id, course_id, title, description, due_at, max_grade::text, created_by, created_at, updated_at
	`, assignmentID, title, description, dueAt, maxGradeValue).Scan(
		&assignment.ID,
		&assignment.CourseID,
		&assignment.Title,
		&assignment.Description,
		&assignment.DueAt,
		&assignment.MaxGrade,
		&assignment.CreatedBy,
		&assignment.CreatedAt,
		&assignment.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAssignmentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update assignment: %w", err)
	}

	assignment.MaxGrade = maxGradeDisplay
	assignment.MetadataHash, err = computeAssignmentMetadataHash(assignment)
	if err != nil {
		return nil, err
	}
	if _, err := appendBlockchainEventTx(ctx, tx, buildAssignmentUpdatedBlockchainEvent(assignment, updatedBy)); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit assignment update transaction: %w", err)
	}

	return &assignment, nil
}

// DeleteAssignment removes an assignment and its dependent data.
func DeleteAssignment(ctx context.Context, courseID string, assignmentID string, deletedBy string) error {
	if pool == nil {
		return ErrDatabaseConnectionNotInitialized
	}

	courseID = strings.TrimSpace(courseID)
	assignmentID = strings.TrimSpace(assignmentID)
	deletedBy = strings.TrimSpace(deletedBy)
	if _, err := uuid.Parse(courseID); err != nil {
		return ErrCourseNotFound
	}
	if _, err := uuid.Parse(assignmentID); err != nil {
		return ErrAssignmentNotFound
	}
	if _, err := uuid.Parse(deletedBy); err != nil {
		return ErrInvalidCreatorID
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin assignment delete transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback assignment delete transaction", "error", rollbackErr)
		}
	}()

	if err := requireCourseManagementAccessTx(ctx, tx, deletedBy, courseID); err != nil {
		return err
	}
	if err := acquireBlockchainLockTx(ctx, tx); err != nil {
		return err
	}

	assignment, err := getAssignmentByIDForMutationTx(ctx, tx, assignmentID)
	if err != nil {
		return err
	}
	if assignment.CourseID != courseID {
		return ErrAssignmentNotFound
	}
	if _, err := appendBlockchainEventTx(ctx, tx, buildAssignmentDeletedBlockchainEvent(*assignment, deletedBy)); err != nil {
		return err
	}

	deletedPosition, err := deleteAssignmentOutlineItemTx(ctx, tx, courseID, assignmentID)
	if err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `DELETE FROM assignments WHERE id = $1`, assignmentID)
	if err != nil {
		return fmt.Errorf("failed to delete assignment: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrAssignmentNotFound
	}
	if deletedPosition > 0 {
		if err := compactCourseOutlinePositionsTx(ctx, tx, courseID, deletedPosition); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit assignment delete transaction: %w", err)
	}

	return nil
}

// CanUserViewAssignment reports whether a user can view an assignment.
func CanUserViewAssignment(ctx context.Context, userID string, assignmentID string) (bool, error) {
	if pool == nil {
		return false, ErrDatabaseConnectionNotInitialized
	}

	var allowed bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM users u
			JOIN assignments a ON a.id = $2
			WHERE u.id = $1
			  AND u.deactivated_at IS NULL
			  AND (
				u.role = 'admin'
				OR (
					u.role = 'teacher'
					AND EXISTS (
						SELECT 1
						FROM course_instructors ci
						WHERE ci.course_id = a.course_id
						  AND ci.teacher_id = u.id
					)
				)
				OR (
					u.role = 'student'
					AND EXISTS (
						SELECT 1
						FROM course_enrollments ce
						WHERE ce.course_id = a.course_id
						  AND ce.student_id = u.id
					)
				)
			  )
		)
	`, userID, assignmentID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("failed to check assignment view access: %w", err)
	}

	return allowed, nil
}

// CanUserManageAssignment reports whether a user can manage an assignment.
func CanUserManageAssignment(ctx context.Context, userID string, assignmentID string) (bool, error) {
	if pool == nil {
		return false, ErrDatabaseConnectionNotInitialized
	}

	var allowed bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM users u
			JOIN assignments a ON a.id = $2
			WHERE u.id = $1
			  AND u.deactivated_at IS NULL
			  AND (
				u.role = 'admin'
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
	`, userID, assignmentID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("failed to check assignment management access: %w", err)
	}

	return allowed, nil
}

type assignmentPublishedEventData struct {
	AssignmentID string `json:"assignment_id"`
	CourseID     string `json:"course_id"`
	MetadataHash string `json:"metadata_hash"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	DueAt        string `json:"due_at,omitempty"`
	MaxGrade     string `json:"max_grade"`
	PublishedBy  string `json:"published_by"`
}

type assignmentUpdatedEventData struct {
	AssignmentID string `json:"assignment_id"`
	CourseID     string `json:"course_id"`
	MetadataHash string `json:"metadata_hash"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	DueAt        string `json:"due_at,omitempty"`
	MaxGrade     string `json:"max_grade"`
	UpdatedBy    string `json:"updated_by"`
}

type assignmentDeletedEventData struct {
	AssignmentID string `json:"assignment_id"`
	CourseID     string `json:"course_id"`
	MetadataHash string `json:"metadata_hash"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	DueAt        string `json:"due_at,omitempty"`
	MaxGrade     string `json:"max_grade"`
	DeletedBy    string `json:"deleted_by"`
}

type assignmentMetadataHashDocument struct {
	CourseID    string `json:"course_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	DueAt       string `json:"due_at,omitempty"`
	MaxGrade    string `json:"max_grade"`
}

func buildAssignmentPublishedBlockchainEvent(assignment Assignment) AppendBlockchainEventInput {
	payload := assignmentPublishedEventData{
		AssignmentID: assignment.ID,
		CourseID:     assignment.CourseID,
		MetadataHash: assignment.MetadataHash,
		Title:        assignment.Title,
		Description:  assignment.Description,
		MaxGrade:     assignment.MaxGrade,
		PublishedBy:  assignment.CreatedBy,
	}
	if assignment.DueAt != nil {
		payload.DueAt = formatBlockchainTime(*assignment.DueAt)
	}

	return AppendBlockchainEventInput{
		EventType:   "assignment_published",
		EntityType:  "assignment",
		EntityID:    assignment.ID,
		ActorUserID: assignment.CreatedBy,
		OccurredAt:  assignment.CreatedAt,
		Data:        payload,
	}
}

func buildAssignmentUpdatedBlockchainEvent(assignment Assignment, updatedBy string) AppendBlockchainEventInput {
	payload := assignmentUpdatedEventData{
		AssignmentID: assignment.ID,
		CourseID:     assignment.CourseID,
		MetadataHash: assignment.MetadataHash,
		Title:        assignment.Title,
		Description:  assignment.Description,
		MaxGrade:     assignment.MaxGrade,
		UpdatedBy:    updatedBy,
	}
	if assignment.DueAt != nil {
		payload.DueAt = formatBlockchainTime(*assignment.DueAt)
	}

	return AppendBlockchainEventInput{
		EventType:   "assignment_updated",
		EntityType:  "assignment",
		EntityID:    assignment.ID,
		ActorUserID: updatedBy,
		OccurredAt:  assignment.UpdatedAt,
		Data:        payload,
	}
}

func buildAssignmentDeletedBlockchainEvent(assignment Assignment, deletedBy string) AppendBlockchainEventInput {
	payload := assignmentDeletedEventData{
		AssignmentID: assignment.ID,
		CourseID:     assignment.CourseID,
		MetadataHash: assignment.MetadataHash,
		Title:        assignment.Title,
		Description:  assignment.Description,
		MaxGrade:     assignment.MaxGrade,
		DeletedBy:    deletedBy,
	}
	if assignment.DueAt != nil {
		payload.DueAt = formatBlockchainTime(*assignment.DueAt)
	}

	return AppendBlockchainEventInput{
		EventType:   "assignment_deleted",
		EntityType:  "assignment",
		EntityID:    assignment.ID,
		ActorUserID: deletedBy,
		OccurredAt:  time.Now().UTC(),
		Data:        payload,
	}
}

func computeAssignmentMetadataHash(assignment Assignment) (string, error) {
	doc := assignmentMetadataHashDocument{
		CourseID:    assignment.CourseID,
		Title:       assignment.Title,
		Description: assignment.Description,
		MaxGrade:    assignment.MaxGrade,
	}
	if assignment.DueAt != nil {
		doc.DueAt = formatBlockchainTime(*assignment.DueAt)
	}

	data, err := marshalCanonicalJSON(doc)
	if err != nil {
		return "", fmt.Errorf("failed to marshal assignment metadata: %w", err)
	}

	return hashBytesSHA256Hex(data), nil
}

func scanAssignment(row pgx.Row) (Assignment, error) {
	var assignment Assignment
	err := row.Scan(
		&assignment.ID,
		&assignment.CourseID,
		&assignment.Title,
		&assignment.Description,
		&assignment.DueAt,
		&assignment.MaxGrade,
		&assignment.CreatedBy,
		&assignment.CreatedAt,
		&assignment.UpdatedAt,
	)
	if err != nil {
		return Assignment{}, err
	}

	assignment.MetadataHash, err = computeAssignmentMetadataHash(assignment)
	if err != nil {
		return Assignment{}, err
	}

	return assignment, nil
}

func normalizeAssignmentMaxGrade(raw string) (float64, string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, "", ErrAssignmentMaxGradeRequired
	}

	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, "", ErrAssignmentMaxGradeInvalid
	}
	if value < 0 {
		return 0, "", ErrAssignmentMaxGradeInvalid
	}

	return value, strconv.FormatFloat(value, 'f', -1, 64), nil
}

func requireCourseManagementAccessTx(ctx context.Context, tx pgx.Tx, userID string, courseID string) error {
	var role string
	err := tx.QueryRow(ctx, `SELECT role FROM users WHERE id = $1 AND deactivated_at IS NULL`, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to load course manager role: %w", err)
	}

	normalizedRole, err := NormalizeRole(role)
	if err != nil {
		return err
	}
	if normalizedRole.IsAdmin() {
		return nil
	}
	if !normalizedRole.IsTeacher() {
		return ErrTeacherRequired
	}

	var allowed bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM course_instructors
			WHERE course_id = $1
			  AND teacher_id = $2
		)
	`, courseID, userID).Scan(&allowed)
	if err != nil {
		return fmt.Errorf("failed to check course management access: %w", err)
	}
	if !allowed {
		return ErrAccessDenied
	}

	return nil
}

func getAssignmentCourseIDTx(ctx context.Context, tx pgx.Tx, assignmentID string) (string, error) {
	var courseID string
	err := tx.QueryRow(ctx, `SELECT course_id FROM assignments WHERE id = $1`, assignmentID).Scan(&courseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrAssignmentNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to load assignment course: %w", err)
	}

	return courseID, nil
}

func getAssignmentByIDForMutationTx(ctx context.Context, tx pgx.Tx, assignmentID string) (*Assignment, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, course_id, title, description, due_at, max_grade::text, created_by, created_at, updated_at
		FROM assignments
		WHERE id = $1
	`, assignmentID)

	assignment, err := scanAssignment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAssignmentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get assignment: %w", err)
	}

	return &assignment, nil
}

func deleteAssignmentOutlineItemTx(ctx context.Context, tx pgx.Tx, courseID string, assignmentID string) (int, error) {
	var position int
	err := tx.QueryRow(ctx, `
		DELETE FROM course_outline_items
		WHERE course_id = $1
		  AND assignment_id = $2
		RETURNING position
	`, courseID, assignmentID).Scan(&position)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to delete assignment outline item: %w", err)
	}

	return position, nil
}

func compactCourseOutlinePositionsTx(ctx context.Context, tx pgx.Tx, courseID string, removedPosition int) error {
	if _, err := tx.Exec(ctx, `
		UPDATE course_outline_items
		SET position = position - 1
		WHERE course_id = $1
		  AND position > $2
	`, courseID, removedPosition); err != nil {
		return fmt.Errorf("failed to compact course outline positions: %w", err)
	}

	return nil
}
