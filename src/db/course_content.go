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
)

type CourseOutlineItemType string

const (
	CourseOutlineItemTypeUnit       CourseOutlineItemType = "unit"
	CourseOutlineItemTypeAssignment CourseOutlineItemType = "assignment"
)

// CourseUnit represents a unit within a course.
type CourseUnit struct {
	ID          string
	CourseID    string
	Title       string
	Description string
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// UnitLesson represents a lesson within a unit.
type UnitLesson struct {
	ID          string
	UnitID      string
	CourseID    string
	UnitTitle   string
	Title       string
	Description string
	Position    int
	SlideCount  int
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// LessonSlide represents one markdown-authored slide.
type LessonSlide struct {
	ID          string
	LessonID    string
	Title       string
	MarkdownRaw string
	Position    int
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// LessonCompletion represents a student completing a lesson.
type LessonCompletion struct {
	ID          string
	LessonID    string
	StudentID   string
	CompletedAt time.Time
}

// CourseOutlineItem represents a course outline entry.
type CourseOutlineItem struct {
	ID         string
	CourseID   string
	Position   int
	ItemType   CourseOutlineItemType
	Unit       *CourseUnit
	Assignment *Assignment
}

// CreateCourseUnitInput defines fields for creating a unit.
type CreateCourseUnitInput struct {
	CourseID          string
	Title             string
	Description       string
	InsertAfterItemID string
	CreatedBy         string
}

// CreateUnitLessonInput defines fields for creating a lesson.
type CreateUnitLessonInput struct {
	CourseID    string
	UnitID      string
	Title       string
	Description string
	CreatedBy   string
}

// UpdateUnitLessonInput defines fields for updating a lesson.
type UpdateUnitLessonInput struct {
	CourseID    string
	LessonID    string
	Title       string
	Description string
	UpdatedBy   string
}

// CreateLessonSlideInput defines fields for creating a slide.
type CreateLessonSlideInput struct {
	CourseID    string
	LessonID    string
	Title       string
	MarkdownRaw string
	CreatedBy   string
}

// UpdateLessonSlideInput defines fields for updating a slide.
type UpdateLessonSlideInput struct {
	CourseID    string
	LessonID    string
	SlideID     string
	Title       string
	MarkdownRaw string
	UpdatedBy   string
}

// DeleteLessonSlideInput defines fields for deleting a slide.
type DeleteLessonSlideInput struct {
	CourseID  string
	LessonID  string
	SlideID   string
	DeletedBy string
}

// MarkLessonCompleteInput defines fields for recording lesson completion.
type MarkLessonCompleteInput struct {
	LessonID  string
	StudentID string
}

// ListCourseOutline returns ordered course content and assignments.
func ListCourseOutline(ctx context.Context, courseID string) ([]CourseOutlineItem, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	rows, err := pool.Query(ctx, `
		SELECT
			coi.id,
			coi.course_id,
			coi.position,
			coi.item_type,
			u.id,
			u.course_id,
			u.title,
			u.description,
			u.created_by,
			u.created_at,
			u.updated_at,
			a.id,
			a.course_id,
			a.title,
			a.description,
			a.due_at,
			a.max_grade::text,
			a.created_by,
			a.created_at,
			a.updated_at
		FROM course_outline_items coi
		LEFT JOIN course_units u ON u.id = coi.unit_id
		LEFT JOIN assignments a ON a.id = coi.assignment_id
		WHERE coi.course_id = $1
		ORDER BY coi.position ASC
	`, courseID)
	if err != nil {
		return nil, fmt.Errorf("failed to list course outline: %w", err)
	}
	defer rows.Close()

	items := make([]CourseOutlineItem, 0)
	for rows.Next() {
		item, err := scanCourseOutlineItem(rows)
		if err != nil {
			return nil, err
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating course outline: %w", err)
	}

	return items, nil
}

// CreateCourseUnit creates a unit and places it in the course outline.
func CreateCourseUnit(ctx context.Context, input CreateCourseUnitInput) (*CourseUnit, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	courseID := strings.TrimSpace(input.CourseID)
	title := strings.TrimSpace(input.Title)
	description := strings.TrimSpace(input.Description)
	createdBy := strings.TrimSpace(input.CreatedBy)

	if _, err := uuid.Parse(courseID); err != nil {
		return nil, ErrCourseNotFound
	}
	if title == "" {
		return nil, ErrCourseUnitTitleRequired
	}
	if _, err := uuid.Parse(createdBy); err != nil {
		return nil, ErrInvalidCreatorID
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin course unit transaction: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback course unit transaction", "error", rollbackErr)
		}
	}()

	if err := ensureCourseExistsTx(ctx, tx, courseID); err != nil {
		return nil, err
	}
	if err := requireCourseManagementAccessTx(ctx, tx, createdBy, courseID); err != nil {
		return nil, err
	}

	position, err := resolveOutlineInsertPositionTx(ctx, tx, courseID, strings.TrimSpace(input.InsertAfterItemID))
	if err != nil {
		return nil, err
	}

	var unit CourseUnit
	err = tx.QueryRow(ctx, `
		INSERT INTO course_units (course_id, title, description, created_by)
		VALUES ($1, $2, $3, $4)
		RETURNING id, course_id, title, description, created_by, created_at, updated_at
	`, courseID, title, description, createdBy).Scan(
		&unit.ID,
		&unit.CourseID,
		&unit.Title,
		&unit.Description,
		&unit.CreatedBy,
		&unit.CreatedAt,
		&unit.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create course unit: %w", err)
	}

	if err := insertOutlineItemTx(ctx, tx, courseID, position, CourseOutlineItemTypeUnit, &unit.ID, nil); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit course unit transaction: %w", err)
	}

	return &unit, nil
}

// ListUnitLessons returns ordered lessons for a unit.
func ListUnitLessons(ctx context.Context, unitID string) ([]UnitLesson, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	rows, err := pool.Query(ctx, `
		SELECT
			ul.id,
			ul.unit_id,
			u.course_id,
			u.title,
			ul.title,
			ul.description,
			ul.position,
			(SELECT COUNT(*) FROM lesson_slides ls WHERE ls.lesson_id = ul.id),
			ul.created_by,
			ul.created_at,
			ul.updated_at
		FROM unit_lessons ul
		JOIN course_units u ON u.id = ul.unit_id
		WHERE ul.unit_id = $1
		ORDER BY ul.position ASC
	`, unitID)
	if err != nil {
		return nil, fmt.Errorf("failed to list unit lessons: %w", err)
	}
	defer rows.Close()

	lessons := make([]UnitLesson, 0)
	for rows.Next() {
		var lesson UnitLesson
		if err := rows.Scan(
			&lesson.ID,
			&lesson.UnitID,
			&lesson.CourseID,
			&lesson.UnitTitle,
			&lesson.Title,
			&lesson.Description,
			&lesson.Position,
			&lesson.SlideCount,
			&lesson.CreatedBy,
			&lesson.CreatedAt,
			&lesson.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan unit lesson: %w", err)
		}

		lessons = append(lessons, lesson)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating unit lessons: %w", err)
	}

	return lessons, nil
}

// CreateUnitLesson creates a lesson within a unit.
func CreateUnitLesson(ctx context.Context, input CreateUnitLessonInput) (*UnitLesson, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	courseID := strings.TrimSpace(input.CourseID)
	unitID := strings.TrimSpace(input.UnitID)
	title := strings.TrimSpace(input.Title)
	description := strings.TrimSpace(input.Description)
	createdBy := strings.TrimSpace(input.CreatedBy)

	if _, err := uuid.Parse(courseID); err != nil {
		return nil, ErrCourseNotFound
	}
	if _, err := uuid.Parse(unitID); err != nil {
		return nil, ErrCourseUnitNotFound
	}
	if title == "" {
		return nil, ErrUnitLessonTitleRequired
	}
	if _, err := uuid.Parse(createdBy); err != nil {
		return nil, ErrInvalidCreatorID
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin lesson transaction: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback lesson transaction", "error", rollbackErr)
		}
	}()

	if err := ensureCourseExistsTx(ctx, tx, courseID); err != nil {
		return nil, err
	}
	if err := ensureUnitBelongsToCourseTx(ctx, tx, unitID, courseID); err != nil {
		return nil, err
	}
	if err := requireCourseManagementAccessTx(ctx, tx, createdBy, courseID); err != nil {
		return nil, err
	}

	position, err := nextSequentialPositionTx(ctx, tx, `SELECT COALESCE(MAX(position), 0) + 1 FROM unit_lessons WHERE unit_id = $1`, unitID)
	if err != nil {
		return nil, err
	}

	var lesson UnitLesson
	err = tx.QueryRow(ctx, `
		INSERT INTO unit_lessons (unit_id, title, description, position, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, unit_id, title, description, position, created_by, created_at, updated_at
	`, unitID, title, description, position, createdBy).Scan(
		&lesson.ID,
		&lesson.UnitID,
		&lesson.Title,
		&lesson.Description,
		&lesson.Position,
		&lesson.CreatedBy,
		&lesson.CreatedAt,
		&lesson.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create unit lesson: %w", err)
	}

	lesson.CourseID = courseID
	lesson.SlideCount = 0

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit lesson transaction: %w", err)
	}

	return &lesson, nil
}

// UpdateUnitLesson updates one lesson within a unit.
func UpdateUnitLesson(ctx context.Context, input UpdateUnitLessonInput) (*UnitLesson, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	courseID := strings.TrimSpace(input.CourseID)
	lessonID := strings.TrimSpace(input.LessonID)
	title := strings.TrimSpace(input.Title)
	description := strings.TrimSpace(input.Description)
	updatedBy := strings.TrimSpace(input.UpdatedBy)

	if _, err := uuid.Parse(courseID); err != nil {
		return nil, ErrCourseNotFound
	}
	if _, err := uuid.Parse(lessonID); err != nil {
		return nil, ErrUnitLessonNotFound
	}
	if title == "" {
		return nil, ErrUnitLessonTitleRequired
	}
	if _, err := uuid.Parse(updatedBy); err != nil {
		return nil, ErrInvalidCreatorID
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin lesson update transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback lesson update transaction", "error", rollbackErr)
		}
	}()

	resolvedCourseID, err := getLessonCourseIDTx(ctx, tx, lessonID)
	if err != nil {
		return nil, err
	}
	if resolvedCourseID != courseID {
		return nil, ErrUnitLessonNotFound
	}
	if err := requireCourseManagementAccessTx(ctx, tx, updatedBy, courseID); err != nil {
		return nil, err
	}

	var lesson UnitLesson
	err = tx.QueryRow(ctx, `
		UPDATE unit_lessons ul
		SET title = $2,
		    description = $3,
		    updated_at = NOW()
		FROM course_units cu
		WHERE ul.id = $1
		  AND cu.id = ul.unit_id
		RETURNING ul.id, ul.unit_id, cu.course_id, cu.title, ul.title, ul.description, ul.position,
			(SELECT COUNT(*) FROM lesson_slides ls WHERE ls.lesson_id = ul.id), ul.created_by, ul.created_at, ul.updated_at
	`, lessonID, title, description).Scan(
		&lesson.ID,
		&lesson.UnitID,
		&lesson.CourseID,
		&lesson.UnitTitle,
		&lesson.Title,
		&lesson.Description,
		&lesson.Position,
		&lesson.SlideCount,
		&lesson.CreatedBy,
		&lesson.CreatedAt,
		&lesson.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUnitLessonNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update lesson: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit lesson update transaction: %w", err)
	}

	return &lesson, nil
}

// DeleteUnitLesson deletes one lesson and compacts lesson positions.
func DeleteUnitLesson(ctx context.Context, courseID string, lessonID string, deletedBy string) error {
	if pool == nil {
		return ErrDatabaseConnectionNotInitialized
	}

	courseID = strings.TrimSpace(courseID)
	lessonID = strings.TrimSpace(lessonID)
	deletedBy = strings.TrimSpace(deletedBy)
	if _, err := uuid.Parse(courseID); err != nil {
		return ErrCourseNotFound
	}
	if _, err := uuid.Parse(lessonID); err != nil {
		return ErrUnitLessonNotFound
	}
	if _, err := uuid.Parse(deletedBy); err != nil {
		return ErrInvalidCreatorID
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin lesson delete transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback lesson delete transaction", "error", rollbackErr)
		}
	}()

	lesson, err := getLessonByIDTx(ctx, tx, lessonID)
	if err != nil {
		return err
	}
	if lesson.CourseID != courseID {
		return ErrUnitLessonNotFound
	}
	if err := requireCourseManagementAccessTx(ctx, tx, deletedBy, courseID); err != nil {
		return err
	}

	command, err := tx.Exec(ctx, `DELETE FROM unit_lessons WHERE id = $1`, lessonID)
	if err != nil {
		return fmt.Errorf("failed to delete lesson: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrUnitLessonNotFound
	}
	if err := compactLessonPositionsTx(ctx, tx, lesson.UnitID, lesson.Position); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit lesson delete transaction: %w", err)
	}

	return nil
}

// GetCourseUnitByID returns one course unit by its ID.
func GetCourseUnitByID(ctx context.Context, unitID string) (*CourseUnit, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	var unit CourseUnit
	err := pool.QueryRow(ctx, `
		SELECT id, course_id, title, description, created_by, created_at, updated_at
		FROM course_units
		WHERE id = $1
	`, unitID).Scan(
		&unit.ID,
		&unit.CourseID,
		&unit.Title,
		&unit.Description,
		&unit.CreatedBy,
		&unit.CreatedAt,
		&unit.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCourseUnitNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get course unit: %w", err)
	}

	return &unit, nil
}

// GetLessonByID returns one lesson and its parent metadata.
func GetLessonByID(ctx context.Context, lessonID string) (*UnitLesson, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	var lesson UnitLesson
	err := pool.QueryRow(ctx, `
		SELECT
			ul.id,
			ul.unit_id,
			u.course_id,
			u.title,
			ul.title,
			ul.description,
			ul.position,
			(SELECT COUNT(*) FROM lesson_slides ls WHERE ls.lesson_id = ul.id),
			ul.created_by,
			ul.created_at,
			ul.updated_at
		FROM unit_lessons ul
		JOIN course_units u ON u.id = ul.unit_id
		WHERE ul.id = $1
	`, lessonID).Scan(
		&lesson.ID,
		&lesson.UnitID,
		&lesson.CourseID,
		&lesson.UnitTitle,
		&lesson.Title,
		&lesson.Description,
		&lesson.Position,
		&lesson.SlideCount,
		&lesson.CreatedBy,
		&lesson.CreatedAt,
		&lesson.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUnitLessonNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get lesson: %w", err)
	}

	return &lesson, nil
}

// ListLessonSlides returns ordered slides for a lesson.
func ListLessonSlides(ctx context.Context, lessonID string) ([]LessonSlide, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	rows, err := pool.Query(ctx, `
		SELECT id, lesson_id, title, markdown_raw, position, created_by, created_at, updated_at
		FROM lesson_slides
		WHERE lesson_id = $1
		ORDER BY position ASC
	`, lessonID)
	if err != nil {
		return nil, fmt.Errorf("failed to list lesson slides: %w", err)
	}
	defer rows.Close()

	slides := make([]LessonSlide, 0)
	for rows.Next() {
		var slide LessonSlide
		if err := rows.Scan(
			&slide.ID,
			&slide.LessonID,
			&slide.Title,
			&slide.MarkdownRaw,
			&slide.Position,
			&slide.CreatedBy,
			&slide.CreatedAt,
			&slide.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan lesson slide: %w", err)
		}

		slides = append(slides, slide)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating lesson slides: %w", err)
	}

	return slides, nil
}

// CreateLessonSlide creates a new slide within a lesson.
func CreateLessonSlide(ctx context.Context, input CreateLessonSlideInput) (*LessonSlide, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	courseID := strings.TrimSpace(input.CourseID)
	lessonID := strings.TrimSpace(input.LessonID)
	title := strings.TrimSpace(input.Title)
	markdownRaw := strings.TrimSpace(input.MarkdownRaw)
	createdBy := strings.TrimSpace(input.CreatedBy)

	if _, err := uuid.Parse(courseID); err != nil {
		return nil, ErrCourseNotFound
	}
	if _, err := uuid.Parse(lessonID); err != nil {
		return nil, ErrUnitLessonNotFound
	}
	if markdownRaw == "" {
		return nil, ErrLessonSlideMarkdownRequired
	}
	if _, err := uuid.Parse(createdBy); err != nil {
		return nil, ErrInvalidCreatorID
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin lesson slide transaction: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback lesson slide transaction", "error", rollbackErr)
		}
	}()

	resolvedCourseID, err := getLessonCourseIDTx(ctx, tx, lessonID)
	if err != nil {
		return nil, err
	}
	if resolvedCourseID != courseID {
		return nil, ErrUnitLessonNotFound
	}
	if err := requireCourseManagementAccessTx(ctx, tx, createdBy, courseID); err != nil {
		return nil, err
	}

	position, err := nextSequentialPositionTx(ctx, tx, `SELECT COALESCE(MAX(position), 0) + 1 FROM lesson_slides WHERE lesson_id = $1`, lessonID)
	if err != nil {
		return nil, err
	}

	var slide LessonSlide
	err = tx.QueryRow(ctx, `
		INSERT INTO lesson_slides (lesson_id, title, markdown_raw, position, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, lesson_id, title, markdown_raw, position, created_by, created_at, updated_at
	`, lessonID, title, markdownRaw, position, createdBy).Scan(
		&slide.ID,
		&slide.LessonID,
		&slide.Title,
		&slide.MarkdownRaw,
		&slide.Position,
		&slide.CreatedBy,
		&slide.CreatedAt,
		&slide.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create lesson slide: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit lesson slide transaction: %w", err)
	}

	return &slide, nil
}

// UpdateLessonSlide updates one existing slide.
func UpdateLessonSlide(ctx context.Context, input UpdateLessonSlideInput) (*LessonSlide, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	courseID := strings.TrimSpace(input.CourseID)
	lessonID := strings.TrimSpace(input.LessonID)
	slideID := strings.TrimSpace(input.SlideID)
	title := strings.TrimSpace(input.Title)
	markdownRaw := strings.TrimSpace(input.MarkdownRaw)
	updatedBy := strings.TrimSpace(input.UpdatedBy)

	if _, err := uuid.Parse(courseID); err != nil {
		return nil, ErrCourseNotFound
	}
	if _, err := uuid.Parse(lessonID); err != nil {
		return nil, ErrUnitLessonNotFound
	}
	if _, err := uuid.Parse(slideID); err != nil {
		return nil, ErrLessonSlideNotFound
	}
	if markdownRaw == "" {
		return nil, ErrLessonSlideMarkdownRequired
	}
	if _, err := uuid.Parse(updatedBy); err != nil {
		return nil, ErrInvalidCreatorID
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin slide update transaction: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback slide update transaction", "error", rollbackErr)
		}
	}()

	resolvedCourseID, err := getLessonCourseIDTx(ctx, tx, lessonID)
	if err != nil {
		return nil, err
	}
	if resolvedCourseID != courseID {
		return nil, ErrUnitLessonNotFound
	}
	if err := requireCourseManagementAccessTx(ctx, tx, updatedBy, courseID); err != nil {
		return nil, err
	}

	resolvedLessonID, err := getSlideLessonIDTx(ctx, tx, slideID)
	if err != nil {
		return nil, err
	}
	if resolvedLessonID != lessonID {
		return nil, ErrLessonSlideNotFound
	}

	var slide LessonSlide
	err = tx.QueryRow(ctx, `
		UPDATE lesson_slides
		SET title = $2,
			markdown_raw = $3,
			updated_at = NOW()
		WHERE id = $1
		RETURNING id, lesson_id, title, markdown_raw, position, created_by, created_at, updated_at
	`, slideID, title, markdownRaw).Scan(
		&slide.ID,
		&slide.LessonID,
		&slide.Title,
		&slide.MarkdownRaw,
		&slide.Position,
		&slide.CreatedBy,
		&slide.CreatedAt,
		&slide.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLessonSlideNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update lesson slide: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit slide update transaction: %w", err)
	}

	return &slide, nil
}

// DeleteLessonSlide deletes one slide and compacts slide positions.
func DeleteLessonSlide(ctx context.Context, input DeleteLessonSlideInput) error {
	if pool == nil {
		return ErrDatabaseConnectionNotInitialized
	}

	courseID := strings.TrimSpace(input.CourseID)
	lessonID := strings.TrimSpace(input.LessonID)
	slideID := strings.TrimSpace(input.SlideID)
	deletedBy := strings.TrimSpace(input.DeletedBy)

	if _, err := uuid.Parse(courseID); err != nil {
		return ErrCourseNotFound
	}
	if _, err := uuid.Parse(lessonID); err != nil {
		return ErrUnitLessonNotFound
	}
	if _, err := uuid.Parse(slideID); err != nil {
		return ErrLessonSlideNotFound
	}
	if _, err := uuid.Parse(deletedBy); err != nil {
		return ErrInvalidCreatorID
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin slide delete transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback slide delete transaction", "error", rollbackErr)
		}
	}()

	resolvedCourseID, err := getLessonCourseIDTx(ctx, tx, lessonID)
	if err != nil {
		return err
	}
	if resolvedCourseID != courseID {
		return ErrUnitLessonNotFound
	}
	if err := requireCourseManagementAccessTx(ctx, tx, deletedBy, courseID); err != nil {
		return err
	}

	slide, err := getLessonSlideByIDTx(ctx, tx, slideID)
	if err != nil {
		return err
	}
	if slide.LessonID != lessonID {
		return ErrLessonSlideNotFound
	}

	command, err := tx.Exec(ctx, `DELETE FROM lesson_slides WHERE id = $1`, slideID)
	if err != nil {
		return fmt.Errorf("failed to delete lesson slide: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrLessonSlideNotFound
	}
	if err := compactSlidePositionsTx(ctx, tx, lessonID, slide.Position); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit slide delete transaction: %w", err)
	}

	return nil
}

// MarkLessonComplete records a completed lesson for a student.
func MarkLessonComplete(ctx context.Context, input MarkLessonCompleteInput) (*LessonCompletion, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	lessonID := strings.TrimSpace(input.LessonID)
	studentID := strings.TrimSpace(input.StudentID)
	if _, err := uuid.Parse(lessonID); err != nil {
		return nil, ErrUnitLessonNotFound
	}
	if _, err := uuid.Parse(studentID); err != nil {
		return nil, ErrUserNotFound
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin lesson completion transaction: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback lesson completion transaction", "error", rollbackErr)
		}
	}()

	courseID, err := getLessonCourseIDTx(ctx, tx, lessonID)
	if err != nil {
		return nil, err
	}
	if err := requireUserRoleTx(ctx, tx, studentID, RoleStudent); err != nil {
		return nil, err
	}
	if err := requireStudentEnrolledTx(ctx, tx, studentID, courseID); err != nil {
		return nil, err
	}

	var completion LessonCompletion
	err = tx.QueryRow(ctx, `
		INSERT INTO lesson_completions (lesson_id, student_id)
		VALUES ($1, $2)
		ON CONFLICT (lesson_id, student_id)
		DO UPDATE SET completed_at = NOW()
		RETURNING id, lesson_id, student_id, completed_at
	`, lessonID, studentID).Scan(
		&completion.ID,
		&completion.LessonID,
		&completion.StudentID,
		&completion.CompletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to mark lesson complete: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit lesson completion transaction: %w", err)
	}

	return &completion, nil
}

// HasStudentCompletedLesson reports whether a student has completed a lesson.
func HasStudentCompletedLesson(ctx context.Context, lessonID string, studentID string) (bool, error) {
	if pool == nil {
		return false, ErrDatabaseConnectionNotInitialized
	}

	var completed bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM lesson_completions
			WHERE lesson_id = $1
			  AND student_id = $2
		)
	`, strings.TrimSpace(lessonID), strings.TrimSpace(studentID)).Scan(&completed)
	if err != nil {
		return false, fmt.Errorf("failed to check lesson completion: %w", err)
	}

	return completed, nil
}

// ListStudentCompletedLessonIDs returns the IDs of lessons in a course that a
// student has completed.
func ListStudentCompletedLessonIDs(ctx context.Context, courseID, studentID string) ([]string, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	rows, err := pool.Query(ctx, `
		SELECT lc.lesson_id
		FROM lesson_completions lc
		JOIN course_lessons cl ON cl.id = lc.lesson_id
		JOIN course_units cu ON cu.id = cl.unit_id
		WHERE cu.course_id = $1
		  AND lc.student_id = $2
	`, strings.TrimSpace(courseID), strings.TrimSpace(studentID))
	if err != nil {
		return nil, fmt.Errorf("failed to list completed lessons: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan lesson id: %w", err)
		}
		ids = append(ids, id)
	}

	return ids, rows.Err()
}

// CanUserViewLesson reports whether a user can view a lesson.
func CanUserViewLesson(ctx context.Context, userID string, lessonID string) (bool, error) {
	if pool == nil {
		return false, ErrDatabaseConnectionNotInitialized
	}

	var allowed bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM users u
			JOIN unit_lessons ul ON ul.id = $2
			JOIN course_units cu ON cu.id = ul.unit_id
			WHERE u.id = $1
			  AND u.deactivated_at IS NULL
			  AND (
				u.role = 'admin'
				OR (
					u.role = 'teacher'
					AND EXISTS (
						SELECT 1
						FROM course_instructors ci
						WHERE ci.course_id = cu.course_id
						  AND ci.teacher_id = u.id
					)
				)
				OR (
					u.role = 'student'
					AND EXISTS (
						SELECT 1
						FROM course_enrollments ce
						WHERE ce.course_id = cu.course_id
						  AND ce.student_id = u.id
					)
				)
			  )
		)
	`, userID, lessonID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("failed to check lesson view access: %w", err)
	}

	return allowed, nil
}

// CanUserManageLesson reports whether a user can manage a lesson.
func CanUserManageLesson(ctx context.Context, userID string, lessonID string) (bool, error) {
	if pool == nil {
		return false, ErrDatabaseConnectionNotInitialized
	}

	var allowed bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM users u
			JOIN unit_lessons ul ON ul.id = $2
			JOIN course_units cu ON cu.id = ul.unit_id
			WHERE u.id = $1
			  AND u.deactivated_at IS NULL
			  AND (
				u.role = 'admin'
				OR (
					u.role = 'teacher'
					AND EXISTS (
						SELECT 1
						FROM course_instructors ci
						WHERE ci.course_id = cu.course_id
						  AND ci.teacher_id = u.id
					)
				)
			  )
		)
	`, userID, lessonID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("failed to check lesson management access: %w", err)
	}

	return allowed, nil
}

func AddAssignmentToCourseOutlineTx(ctx context.Context, tx pgx.Tx, courseID string, assignmentID string, insertAfterItemID string) error {
	position, err := resolveOutlineInsertPositionTx(ctx, tx, courseID, strings.TrimSpace(insertAfterItemID))
	if err != nil {
		return err
	}

	return insertOutlineItemTx(ctx, tx, courseID, position, CourseOutlineItemTypeAssignment, nil, &assignmentID)
}

func scanCourseOutlineItem(row pgx.Row) (CourseOutlineItem, error) {
	var (
		item CourseOutlineItem

		rawItemType string

		unitID          *string
		unitCourseID    *string
		unitTitle       *string
		unitDescription *string
		unitCreatedBy   *string
		unitCreatedAt   *time.Time
		unitUpdatedAt   *time.Time

		assignmentID          *string
		assignmentCourseID    *string
		assignmentTitle       *string
		assignmentDescription *string
		assignmentDueAt       *time.Time
		assignmentMaxGrade    *string
		assignmentCreatedBy   *string
		assignmentCreatedAt   *time.Time
		assignmentUpdatedAt   *time.Time
	)

	err := row.Scan(
		&item.ID,
		&item.CourseID,
		&item.Position,
		&rawItemType,
		&unitID,
		&unitCourseID,
		&unitTitle,
		&unitDescription,
		&unitCreatedBy,
		&unitCreatedAt,
		&unitUpdatedAt,
		&assignmentID,
		&assignmentCourseID,
		&assignmentTitle,
		&assignmentDescription,
		&assignmentDueAt,
		&assignmentMaxGrade,
		&assignmentCreatedBy,
		&assignmentCreatedAt,
		&assignmentUpdatedAt,
	)
	if err != nil {
		return CourseOutlineItem{}, fmt.Errorf("failed to scan course outline item: %w", err)
	}

	item.ItemType = CourseOutlineItemType(strings.TrimSpace(rawItemType))
	if item.ItemType == CourseOutlineItemTypeUnit && unitID != nil && unitCourseID != nil && unitTitle != nil && unitDescription != nil && unitCreatedBy != nil && unitCreatedAt != nil && unitUpdatedAt != nil {
		item.Unit = &CourseUnit{
			ID:          *unitID,
			CourseID:    *unitCourseID,
			Title:       *unitTitle,
			Description: *unitDescription,
			CreatedBy:   *unitCreatedBy,
			CreatedAt:   *unitCreatedAt,
			UpdatedAt:   *unitUpdatedAt,
		}
	}
	if item.ItemType == CourseOutlineItemTypeAssignment && assignmentID != nil && assignmentCourseID != nil && assignmentTitle != nil && assignmentDescription != nil && assignmentMaxGrade != nil && assignmentCreatedBy != nil && assignmentCreatedAt != nil && assignmentUpdatedAt != nil {
		assignment := Assignment{
			ID:          *assignmentID,
			CourseID:    *assignmentCourseID,
			Title:       *assignmentTitle,
			Description: *assignmentDescription,
			DueAt:       assignmentDueAt,
			MaxGrade:    *assignmentMaxGrade,
			CreatedBy:   *assignmentCreatedBy,
			CreatedAt:   *assignmentCreatedAt,
			UpdatedAt:   *assignmentUpdatedAt,
		}

		metadataHash, err := computeAssignmentMetadataHash(assignment)
		if err != nil {
			return CourseOutlineItem{}, err
		}
		assignment.MetadataHash = metadataHash
		item.Assignment = &assignment
	}

	return item, nil
}

func resolveOutlineInsertPositionTx(ctx context.Context, tx pgx.Tx, courseID string, insertAfterItemID string) (int, error) {
	if strings.TrimSpace(insertAfterItemID) == "" {
		return nextSequentialPositionTx(ctx, tx, `SELECT COALESCE(MAX(position), 0) + 1 FROM course_outline_items WHERE course_id = $1`, courseID)
	}

	if _, err := uuid.Parse(insertAfterItemID); err != nil {
		return 0, ErrOutlineItemNotFound
	}

	var afterPosition int
	err := tx.QueryRow(ctx, `
		SELECT position
		FROM course_outline_items
		WHERE id = $1
		  AND course_id = $2
	`, insertAfterItemID, courseID).Scan(&afterPosition)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrOutlineItemNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("failed to load outline insertion point: %w", err)
	}

	return afterPosition + 1, nil
}

func insertOutlineItemTx(ctx context.Context, tx pgx.Tx, courseID string, position int, itemType CourseOutlineItemType, unitID *string, assignmentID *string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE course_outline_items
		SET position = position + 1000000
		WHERE course_id = $1
		  AND position >= $2
	`, courseID, position); err != nil {
		return fmt.Errorf("failed to shift course outline positions: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE course_outline_items
		SET position = position - 999999
		WHERE course_id = $1
		  AND position >= $2
	`, courseID, position+1000000); err != nil {
		return fmt.Errorf("failed to normalize course outline positions: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO course_outline_items (course_id, position, item_type, unit_id, assignment_id)
		VALUES ($1, $2, $3, $4, $5)
	`, courseID, position, string(itemType), unitID, assignmentID); err != nil {
		return fmt.Errorf("failed to insert course outline item: %w", err)
	}

	return nil
}

func nextSequentialPositionTx(ctx context.Context, tx pgx.Tx, query string, args ...any) (int, error) {
	var position int
	if err := tx.QueryRow(ctx, query, args...).Scan(&position); err != nil {
		return 0, fmt.Errorf("failed to determine next position: %w", err)
	}

	return position, nil
}

func ensureUnitBelongsToCourseTx(ctx context.Context, tx pgx.Tx, unitID string, courseID string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM course_units
			WHERE id = $1
			  AND course_id = $2
		)
	`, unitID, courseID).Scan(&exists); err != nil {
		return fmt.Errorf("failed to check unit course relationship: %w", err)
	}
	if !exists {
		return ErrCourseUnitNotFound
	}

	return nil
}

func getLessonCourseIDTx(ctx context.Context, tx pgx.Tx, lessonID string) (string, error) {
	var courseID string
	err := tx.QueryRow(ctx, `
		SELECT cu.course_id
		FROM unit_lessons ul
		JOIN course_units cu ON cu.id = ul.unit_id
		WHERE ul.id = $1
	`, lessonID).Scan(&courseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUnitLessonNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to load lesson course: %w", err)
	}

	return courseID, nil
}

func getLessonByIDTx(ctx context.Context, tx pgx.Tx, lessonID string) (*UnitLesson, error) {
	var lesson UnitLesson
	err := tx.QueryRow(ctx, `
		SELECT ul.id, ul.unit_id, cu.course_id, cu.title, ul.title, ul.description, ul.position,
			(SELECT COUNT(*) FROM lesson_slides ls WHERE ls.lesson_id = ul.id), ul.created_by, ul.created_at, ul.updated_at
		FROM unit_lessons ul
		JOIN course_units cu ON cu.id = ul.unit_id
		WHERE ul.id = $1
	`, lessonID).Scan(
		&lesson.ID,
		&lesson.UnitID,
		&lesson.CourseID,
		&lesson.UnitTitle,
		&lesson.Title,
		&lesson.Description,
		&lesson.Position,
		&lesson.SlideCount,
		&lesson.CreatedBy,
		&lesson.CreatedAt,
		&lesson.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUnitLessonNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load lesson: %w", err)
	}

	return &lesson, nil
}

func getSlideLessonIDTx(ctx context.Context, tx pgx.Tx, slideID string) (string, error) {
	var lessonID string
	err := tx.QueryRow(ctx, `SELECT lesson_id FROM lesson_slides WHERE id = $1`, slideID).Scan(&lessonID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrLessonSlideNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to load slide lesson: %w", err)
	}

	return lessonID, nil
}

func getLessonSlideByIDTx(ctx context.Context, tx pgx.Tx, slideID string) (*LessonSlide, error) {
	var slide LessonSlide
	err := tx.QueryRow(ctx, `
		SELECT id, lesson_id, title, markdown_raw, position, created_by, created_at, updated_at
		FROM lesson_slides
		WHERE id = $1
	`, slideID).Scan(
		&slide.ID,
		&slide.LessonID,
		&slide.Title,
		&slide.MarkdownRaw,
		&slide.Position,
		&slide.CreatedBy,
		&slide.CreatedAt,
		&slide.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLessonSlideNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load lesson slide: %w", err)
	}

	return &slide, nil
}

func compactLessonPositionsTx(ctx context.Context, tx pgx.Tx, unitID string, removedPosition int) error {
	if _, err := tx.Exec(ctx, `
		UPDATE unit_lessons
		SET position = position - 1
		WHERE unit_id = $1
		  AND position > $2
	`, unitID, removedPosition); err != nil {
		return fmt.Errorf("failed to compact lesson positions: %w", err)
	}

	return nil
}

func compactSlidePositionsTx(ctx context.Context, tx pgx.Tx, lessonID string, removedPosition int) error {
	if _, err := tx.Exec(ctx, `
		UPDATE lesson_slides
		SET position = position - 1
		WHERE lesson_id = $1
		  AND position > $2
	`, lessonID, removedPosition); err != nil {
		return fmt.Errorf("failed to compact slide positions: %w", err)
	}

	return nil
}
