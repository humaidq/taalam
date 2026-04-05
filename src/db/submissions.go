/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// MaxSubmissionFileSizeBytes is the maximum accepted submission upload size.
const MaxSubmissionFileSizeBytes int64 = 10 << 20

// Submission represents a stored assignment submission.
type Submission struct {
	ID           string
	AssignmentID string
	StudentID    string
	Version      int
	FileName     string
	ContentType  string
	FileSize     int64
	FileSHA256   string
	FileBytes    []byte
	SubmittedAt  time.Time
}

// SubmissionWithStudent represents a submission with student profile metadata.
type SubmissionWithStudent struct {
	Submission
	StudentDisplayName string
	StudentUsername    *string
}

// SubmissionReceipt represents a submission plus the blockchain record that anchored it.
type SubmissionReceipt struct {
	Submission  Submission
	BlockHeight int64
	EventHash   string
	BlockHash   string
}

// CreateSubmissionInput defines fields for uploading a submission.
type CreateSubmissionInput struct {
	AssignmentID string
	StudentID    string
	FileName     string
	ContentType  string
	FileBytes    []byte
}

// CreateSubmission stores a new submission version and appends a blockchain event.
func CreateSubmission(ctx context.Context, input CreateSubmissionInput) (*SubmissionReceipt, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	assignmentID := strings.TrimSpace(input.AssignmentID)
	studentID := strings.TrimSpace(input.StudentID)
	fileName := strings.TrimSpace(filepath.Base(input.FileName))
	contentType := strings.TrimSpace(input.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	if _, err := uuid.Parse(assignmentID); err != nil {
		return nil, ErrAssignmentNotFound
	}
	if _, err := uuid.Parse(studentID); err != nil {
		return nil, ErrUserNotFound
	}
	if fileName == "" || fileName == "." {
		return nil, ErrSubmissionFileNameRequired
	}
	if len(input.FileBytes) == 0 {
		return nil, ErrSubmissionFileRequired
	}
	if int64(len(input.FileBytes)) > MaxSubmissionFileSizeBytes {
		return nil, ErrSubmissionFileTooLarge
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin submission transaction: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback submission transaction", "error", rollbackErr)
		}
	}()

	assignment, err := getAssignmentByIDTx(ctx, tx, assignmentID)
	if err != nil {
		return nil, err
	}
	if err := requireUserRoleTx(ctx, tx, studentID, RoleStudent); err != nil {
		return nil, err
	}
	if err := requireStudentEnrolledTx(ctx, tx, studentID, assignment.CourseID); err != nil {
		return nil, err
	}
	if assignment.DueAt != nil && time.Now().UTC().After(*assignment.DueAt) {
		return nil, ErrSubmissionPastDue
	}
	if err := acquireBlockchainLockTx(ctx, tx); err != nil {
		return nil, err
	}

	version, err := nextSubmissionVersionTx(ctx, tx, assignmentID, studentID)
	if err != nil {
		return nil, err
	}

	fileSHA256 := hashSubmissionBytesSHA256Hex(input.FileBytes)
	fileSize := int64(len(input.FileBytes))

	var submission Submission
	err = tx.QueryRow(ctx, `
		INSERT INTO submissions (
			assignment_id,
			student_id,
			version,
			file_name,
			content_type,
			file_size,
			file_sha256,
			file_bytes
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, assignment_id, student_id, version, file_name, content_type, file_size, file_sha256, file_bytes, submitted_at
	`, assignmentID, studentID, version, fileName, contentType, fileSize, fileSHA256, input.FileBytes).Scan(
		&submission.ID,
		&submission.AssignmentID,
		&submission.StudentID,
		&submission.Version,
		&submission.FileName,
		&submission.ContentType,
		&submission.FileSize,
		&submission.FileSHA256,
		&submission.FileBytes,
		&submission.SubmittedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create submission: %w", err)
	}

	record, err := appendBlockchainEventTx(ctx, tx, buildSubmissionCommittedBlockchainEvent(submission))
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit submission transaction: %w", err)
	}

	return &SubmissionReceipt{
		Submission:  submission,
		BlockHeight: record.Block.Height,
		EventHash:   record.Block.EventHash,
		BlockHash:   record.Block.BlockHash,
	}, nil
}

// GetSubmissionByID returns a submission by ID.
func GetSubmissionByID(ctx context.Context, submissionID string) (*Submission, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	return getSubmissionByIDQuerier(ctx, pool, submissionID)
}

// GetSubmissionWithStudentByID returns a submission with student metadata.
func GetSubmissionWithStudentByID(ctx context.Context, submissionID string) (*SubmissionWithStudent, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	return getSubmissionWithStudentByIDQuerier(ctx, pool, submissionID)
}

// ListSubmissionsForAssignment returns all submissions for an assignment.
func ListSubmissionsForAssignment(ctx context.Context, assignmentID string) ([]SubmissionWithStudent, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	rows, err := pool.Query(ctx, `
		SELECT
			s.id,
			s.assignment_id,
			s.student_id,
			s.version,
			s.file_name,
			s.content_type,
			s.file_size,
			s.file_sha256,
			NULL::bytea,
			s.submitted_at,
			u.display_name,
			u.username
		FROM submissions s
		JOIN users u ON u.id = s.student_id
		WHERE s.assignment_id = $1
		ORDER BY s.submitted_at DESC, s.version DESC
	`, assignmentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list submissions: %w", err)
	}
	defer rows.Close()

	submissions := make([]SubmissionWithStudent, 0)
	for rows.Next() {
		var item SubmissionWithStudent
		if err := rows.Scan(
			&item.ID,
			&item.AssignmentID,
			&item.StudentID,
			&item.Version,
			&item.FileName,
			&item.ContentType,
			&item.FileSize,
			&item.FileSHA256,
			&item.FileBytes,
			&item.SubmittedAt,
			&item.StudentDisplayName,
			&item.StudentUsername,
		); err != nil {
			return nil, fmt.Errorf("failed to scan submission: %w", err)
		}

		submissions = append(submissions, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating submissions: %w", err)
	}

	return submissions, nil
}

// GetLatestSubmissionForStudentAssignment returns the latest submission for one student on one assignment.
func GetLatestSubmissionForStudentAssignment(ctx context.Context, studentID string, assignmentID string) (*Submission, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	return getLatestSubmissionForStudentAssignmentQuerier(ctx, pool, studentID, assignmentID)
}

// GetSubmissionReceipt returns a submission receipt and its blockchain anchor.
func GetSubmissionReceipt(ctx context.Context, submissionID string) (*SubmissionReceipt, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	var receipt SubmissionReceipt
	err := pool.QueryRow(ctx, `
		SELECT
			s.id,
			s.assignment_id,
			s.student_id,
			s.version,
			s.file_name,
			s.content_type,
			s.file_size,
			s.file_sha256,
			s.file_bytes,
			s.submitted_at,
			b.height,
			b.event_hash,
			b.block_hash
		FROM submissions s
		JOIN chain_events e
		  ON e.entity_type = 'submission'
		 AND e.entity_id = s.id::text
		 AND e.event_type = 'submission_committed'
		JOIN chain_blocks b ON b.id = e.block_id
		WHERE s.id = $1
		ORDER BY b.height DESC
		LIMIT 1
	`, submissionID).Scan(
		&receipt.Submission.ID,
		&receipt.Submission.AssignmentID,
		&receipt.Submission.StudentID,
		&receipt.Submission.Version,
		&receipt.Submission.FileName,
		&receipt.Submission.ContentType,
		&receipt.Submission.FileSize,
		&receipt.Submission.FileSHA256,
		&receipt.Submission.FileBytes,
		&receipt.Submission.SubmittedAt,
		&receipt.BlockHeight,
		&receipt.EventHash,
		&receipt.BlockHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSubmissionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get submission receipt: %w", err)
	}

	return &receipt, nil
}

// CanUserSubmitAssignment reports whether a user may submit to an assignment.
func CanUserSubmitAssignment(ctx context.Context, userID string, assignmentID string) (bool, error) {
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
			  AND u.role = 'student'
			  AND EXISTS (
				SELECT 1
				FROM course_enrollments ce
				WHERE ce.course_id = a.course_id
				  AND ce.student_id = u.id
			  )
		)
	`, userID, assignmentID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("failed to check assignment submission access: %w", err)
	}

	return allowed, nil
}

type submissionCommittedEventData struct {
	SubmissionID string `json:"submission_id"`
	AssignmentID string `json:"assignment_id"`
	StudentID    string `json:"student_id"`
	Version      int    `json:"version"`
	FileName     string `json:"file_name"`
	FileHash     string `json:"file_hash"`
	FileSize     int64  `json:"file_size"`
	SubmittedAt  string `json:"submitted_at"`
}

func buildSubmissionCommittedBlockchainEvent(submission Submission) AppendBlockchainEventInput {
	return AppendBlockchainEventInput{
		EventType:   "submission_committed",
		EntityType:  "submission",
		EntityID:    submission.ID,
		ActorUserID: submission.StudentID,
		OccurredAt:  submission.SubmittedAt,
		Data: submissionCommittedEventData{
			SubmissionID: submission.ID,
			AssignmentID: submission.AssignmentID,
			StudentID:    submission.StudentID,
			Version:      submission.Version,
			FileName:     submission.FileName,
			FileHash:     submission.FileSHA256,
			FileSize:     submission.FileSize,
			SubmittedAt:  formatBlockchainTime(submission.SubmittedAt),
		},
	}
}

func getAssignmentByIDTx(ctx context.Context, tx pgx.Tx, assignmentID string) (*Assignment, error) {
	assignment, err := scanAssignment(tx.QueryRow(ctx, `
		SELECT id, course_id, title, description, due_at, max_grade::text, created_by, created_at, updated_at
		FROM assignments
		WHERE id = $1
	`, assignmentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAssignmentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get assignment: %w", err)
	}

	return &assignment, nil
}

func getLatestSubmissionForStudentAssignmentQuerier(ctx context.Context, querier blockchainHeadQuerier, studentID string, assignmentID string) (*Submission, error) {
	row := querier.QueryRow(ctx, `
		SELECT id, assignment_id, student_id, version, file_name, content_type, file_size, file_sha256, file_bytes, submitted_at
		FROM submissions
		WHERE assignment_id = $1
		  AND student_id = $2
		ORDER BY version DESC
		LIMIT 1
	`, assignmentID, studentID)

	var submission Submission
	err := row.Scan(
		&submission.ID,
		&submission.AssignmentID,
		&submission.StudentID,
		&submission.Version,
		&submission.FileName,
		&submission.ContentType,
		&submission.FileSize,
		&submission.FileSHA256,
		&submission.FileBytes,
		&submission.SubmittedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get latest submission: %w", err)
	}

	return &submission, nil
}

func getSubmissionByIDQuerier(ctx context.Context, querier blockchainHeadQuerier, submissionID string) (*Submission, error) {
	row := querier.QueryRow(ctx, `
		SELECT id, assignment_id, student_id, version, file_name, content_type, file_size, file_sha256, file_bytes, submitted_at
		FROM submissions
		WHERE id = $1
	`, submissionID)

	var submission Submission
	err := row.Scan(
		&submission.ID,
		&submission.AssignmentID,
		&submission.StudentID,
		&submission.Version,
		&submission.FileName,
		&submission.ContentType,
		&submission.FileSize,
		&submission.FileSHA256,
		&submission.FileBytes,
		&submission.SubmittedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSubmissionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get submission: %w", err)
	}

	return &submission, nil
}

func getSubmissionWithStudentByIDQuerier(ctx context.Context, querier blockchainHeadQuerier, submissionID string) (*SubmissionWithStudent, error) {
	row := querier.QueryRow(ctx, `
		SELECT s.id, s.assignment_id, s.student_id, s.version, s.file_name, s.content_type, s.file_size, s.file_sha256, s.file_bytes, s.submitted_at, u.display_name, u.username
		FROM submissions s
		JOIN users u ON u.id = s.student_id
		WHERE s.id = $1
	`, submissionID)

	var submission SubmissionWithStudent
	err := row.Scan(
		&submission.ID,
		&submission.AssignmentID,
		&submission.StudentID,
		&submission.Version,
		&submission.FileName,
		&submission.ContentType,
		&submission.FileSize,
		&submission.FileSHA256,
		&submission.FileBytes,
		&submission.SubmittedAt,
		&submission.StudentDisplayName,
		&submission.StudentUsername,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSubmissionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get submission with student metadata: %w", err)
	}

	return &submission, nil
}

func nextSubmissionVersionTx(ctx context.Context, tx pgx.Tx, assignmentID string, studentID string) (int, error) {
	var latestVersion int
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(version), 0)
		FROM submissions
		WHERE assignment_id = $1
		  AND student_id = $2
	`, assignmentID, studentID).Scan(&latestVersion)
	if err != nil {
		return 0, fmt.Errorf("failed to compute next submission version: %w", err)
	}

	return nextSubmissionVersionFromLatest(latestVersion), nil
}

func nextSubmissionVersionFromLatest(latestVersion int) int {
	if latestVersion < 1 {
		return 1
	}

	return latestVersion + 1
}

func requireStudentEnrolledTx(ctx context.Context, tx pgx.Tx, studentID string, courseID string) error {
	var enrolled bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM course_enrollments
			WHERE course_id = $1
			  AND student_id = $2
		)
	`, courseID, studentID).Scan(&enrolled)
	if err != nil {
		return fmt.Errorf("failed to check course enrollment: %w", err)
	}
	if !enrolled {
		return ErrAccessDenied
	}

	return nil
}

func hashSubmissionBytesSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}
