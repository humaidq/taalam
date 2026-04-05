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
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Grade represents a stored grade version.
type Grade struct {
	ID              string
	AssignmentID    string
	StudentID       string
	SubmissionID    string
	Version         int
	GradeValue      string
	FeedbackText    string
	CommitmentHash  string
	PublishedBy     string
	PublishedAt     time.Time
	PreviousGradeID *string
}

// GradeVerification represents the latest grade plus blockchain verification details.
type GradeVerification struct {
	Grade                    Grade
	BlockHeight              int64
	EventHash                string
	BlockHash                string
	EventType                string
	OnChainCommitmentHash    string
	ComputedCommitmentHash   string
	StoredCommitmentMatches  bool
	OnChainCommitmentMatches bool
}

// PublishGradeInput defines fields for publishing or revising a grade.
type PublishGradeInput struct {
	SubmissionID string
	PublishedBy  string
	GradeValue   string
	FeedbackText string
}

// PublishGrade creates a new grade version and emits a blockchain event.
func PublishGrade(ctx context.Context, input PublishGradeInput) (*GradeVerification, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	submissionID := strings.TrimSpace(input.SubmissionID)
	publishedBy := strings.TrimSpace(input.PublishedBy)
	feedbackText := strings.TrimSpace(input.FeedbackText)
	if _, err := uuid.Parse(submissionID); err != nil {
		return nil, ErrSubmissionNotFound
	}
	if _, err := uuid.Parse(publishedBy); err != nil {
		return nil, ErrInvalidCreatorID
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin grade transaction: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback grade transaction", "error", rollbackErr)
		}
	}()

	submission, err := getSubmissionByIDQuerier(ctx, tx, submissionID)
	if err != nil {
		return nil, err
	}
	assignment, err := getAssignmentByIDTx(ctx, tx, submission.AssignmentID)
	if err != nil {
		return nil, err
	}
	if err := requireCourseManagementAccessTx(ctx, tx, publishedBy, assignment.CourseID); err != nil {
		return nil, err
	}
	if err := acquireBlockchainLockTx(ctx, tx); err != nil {
		return nil, err
	}

	gradeValue, gradeValueDisplay, err := normalizeGradeValue(input.GradeValue, assignment.MaxGrade)
	if err != nil {
		return nil, err
	}

	previousGrade, err := getLatestGradeForAssignmentStudentTx(ctx, tx, submission.AssignmentID, submission.StudentID)
	if err != nil {
		return nil, err
	}

	version, previousGradeID, eventType := nextGradeVersion(previousGrade)

	commitmentHash, feedbackHash, err := computeGradeCommitment(submission.AssignmentID, submission.StudentID, submission.ID, gradeValueDisplay, feedbackText, version)
	if err != nil {
		return nil, err
	}

	var grade Grade
	err = tx.QueryRow(ctx, `
		INSERT INTO grades (
			assignment_id,
			student_id,
			submission_id,
			version,
			grade_value,
			feedback_text,
			commitment_hash,
			published_by,
			previous_grade_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, assignment_id, student_id, submission_id, version, grade_value::text, feedback_text, commitment_hash, published_by, published_at, previous_grade_id
	`, submission.AssignmentID, submission.StudentID, submission.ID, version, gradeValue, feedbackText, commitmentHash, publishedBy, previousGradeID).Scan(
		&grade.ID,
		&grade.AssignmentID,
		&grade.StudentID,
		&grade.SubmissionID,
		&grade.Version,
		&grade.GradeValue,
		&grade.FeedbackText,
		&grade.CommitmentHash,
		&grade.PublishedBy,
		&grade.PublishedAt,
		&grade.PreviousGradeID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to publish grade: %w", err)
	}

	record, err := appendBlockchainEventTx(ctx, tx, buildGradeBlockchainEvent(eventType, grade, feedbackHash))
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit grade transaction: %w", err)
	}

	computedHash, _, err := computeGradeCommitment(grade.AssignmentID, grade.StudentID, grade.SubmissionID, grade.GradeValue, grade.FeedbackText, grade.Version)
	if err != nil {
		return nil, err
	}

	return &GradeVerification{
		Grade:                    grade,
		BlockHeight:              record.Block.Height,
		EventHash:                record.Block.EventHash,
		BlockHash:                record.Block.BlockHash,
		EventType:                eventType,
		OnChainCommitmentHash:    grade.CommitmentHash,
		ComputedCommitmentHash:   computedHash,
		StoredCommitmentMatches:  computedHash == grade.CommitmentHash,
		OnChainCommitmentMatches: grade.CommitmentHash == grade.CommitmentHash,
	}, nil
}

// GetLatestGradeForAssignmentStudent returns the latest grade for one student on one assignment.
func GetLatestGradeForAssignmentStudent(ctx context.Context, assignmentID string, studentID string) (*Grade, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	return getLatestGradeForAssignmentStudentTx(ctx, pool, assignmentID, studentID)
}

// GetGradeVerificationForAssignmentStudent returns the latest grade and verification details.
func GetGradeVerificationForAssignmentStudent(ctx context.Context, assignmentID string, studentID string) (*GradeVerification, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	var result GradeVerification
	err := pool.QueryRow(ctx, `
		SELECT
			g.id,
			g.assignment_id,
			g.student_id,
			g.submission_id,
			g.version,
			g.grade_value::text,
			g.feedback_text,
			g.commitment_hash,
			g.published_by,
			g.published_at,
			g.previous_grade_id,
			b.height,
			b.event_hash,
			b.block_hash,
			e.event_type,
			COALESCE(e.payload_json->>'commitment_hash', '')
		FROM grades g
		JOIN chain_events e
		  ON e.entity_type = 'grade'
		 AND e.entity_id = g.id::text
		 AND e.event_type IN ('grade_published', 'grade_revised')
		JOIN chain_blocks b ON b.id = e.block_id
		WHERE g.assignment_id = $1
		  AND g.student_id = $2
		ORDER BY g.version DESC, b.height DESC
		LIMIT 1
	`, assignmentID, studentID).Scan(
		&result.Grade.ID,
		&result.Grade.AssignmentID,
		&result.Grade.StudentID,
		&result.Grade.SubmissionID,
		&result.Grade.Version,
		&result.Grade.GradeValue,
		&result.Grade.FeedbackText,
		&result.Grade.CommitmentHash,
		&result.Grade.PublishedBy,
		&result.Grade.PublishedAt,
		&result.Grade.PreviousGradeID,
		&result.BlockHeight,
		&result.EventHash,
		&result.BlockHash,
		&result.EventType,
		&result.OnChainCommitmentHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrGradeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get grade verification: %w", err)
	}

	computedHash, _, err := computeGradeCommitment(
		result.Grade.AssignmentID,
		result.Grade.StudentID,
		result.Grade.SubmissionID,
		result.Grade.GradeValue,
		result.Grade.FeedbackText,
		result.Grade.Version,
	)
	if err != nil {
		return nil, err
	}
	if computedHash != result.Grade.CommitmentHash {
		legacyHash, _, err := computeLegacyGradeCommitment(
			result.Grade.AssignmentID,
			result.Grade.StudentID,
			result.Grade.SubmissionID,
			result.Grade.GradeValue,
			result.Grade.FeedbackText,
			result.Grade.Version,
		)
		if err != nil {
			return nil, err
		}
		if legacyHash == result.Grade.CommitmentHash {
			computedHash = legacyHash
		}
	}

	result.ComputedCommitmentHash = computedHash
	result.StoredCommitmentMatches = computedHash == result.Grade.CommitmentHash
	result.OnChainCommitmentMatches = result.OnChainCommitmentHash == result.Grade.CommitmentHash

	return &result, nil
}

func getLatestGradeForAssignmentStudentTx(ctx context.Context, querier blockchainHeadQuerier, assignmentID string, studentID string) (*Grade, error) {
	row := querier.QueryRow(ctx, `
		SELECT id, assignment_id, student_id, submission_id, version, grade_value::text, feedback_text, commitment_hash, published_by, published_at, previous_grade_id
		FROM grades
		WHERE assignment_id = $1
		  AND student_id = $2
		ORDER BY version DESC
		LIMIT 1
	`, assignmentID, studentID)

	var grade Grade
	err := row.Scan(
		&grade.ID,
		&grade.AssignmentID,
		&grade.StudentID,
		&grade.SubmissionID,
		&grade.Version,
		&grade.GradeValue,
		&grade.FeedbackText,
		&grade.CommitmentHash,
		&grade.PublishedBy,
		&grade.PublishedAt,
		&grade.PreviousGradeID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get latest grade: %w", err)
	}

	return &grade, nil
}

type gradeCommitmentDocument struct {
	AssignmentID string `json:"assignment_id"`
	StudentID    string `json:"student_id"`
	SubmissionID string `json:"submission_id"`
	GradeValue   string `json:"grade_value"`
	FeedbackHash string `json:"feedback_hash"`
	Version      int    `json:"version"`
}

type gradeBlockchainEventData struct {
	GradeID         string `json:"grade_id"`
	AssignmentID    string `json:"assignment_id"`
	StudentID       string `json:"student_id"`
	SubmissionID    string `json:"submission_id"`
	Version         int    `json:"version"`
	CommitmentHash  string `json:"commitment_hash"`
	FeedbackHash    string `json:"feedback_hash"`
	GradeValue      string `json:"grade_value"`
	PreviousGradeID string `json:"previous_grade_id,omitempty"`
	PublishedBy     string `json:"published_by"`
}

func buildGradeBlockchainEvent(eventType string, grade Grade, feedbackHash string) AppendBlockchainEventInput {
	payload := gradeBlockchainEventData{
		GradeID:        grade.ID,
		AssignmentID:   grade.AssignmentID,
		StudentID:      grade.StudentID,
		SubmissionID:   grade.SubmissionID,
		Version:        grade.Version,
		CommitmentHash: grade.CommitmentHash,
		FeedbackHash:   feedbackHash,
		GradeValue:     grade.GradeValue,
		PublishedBy:    grade.PublishedBy,
	}
	if grade.PreviousGradeID != nil {
		payload.PreviousGradeID = *grade.PreviousGradeID
	}

	return AppendBlockchainEventInput{
		EventType:   eventType,
		EntityType:  "grade",
		EntityID:    grade.ID,
		ActorUserID: grade.PublishedBy,
		OccurredAt:  grade.PublishedAt,
		Data:        payload,
	}
}

func computeGradeCommitment(assignmentID string, studentID string, submissionID string, gradeValue string, feedbackText string, version int) (string, string, error) {
	return computeGradeCommitmentWithFormatter(assignmentID, studentID, submissionID, gradeValue, feedbackText, version, canonicalGradeValueForCommitment)
}

func computeLegacyGradeCommitment(assignmentID string, studentID string, submissionID string, gradeValue string, feedbackText string, version int) (string, string, error) {
	return computeGradeCommitmentWithFormatter(assignmentID, studentID, submissionID, gradeValue, feedbackText, version, legacyGradeValueForCommitment)
}

func computeGradeCommitmentWithFormatter(assignmentID string, studentID string, submissionID string, gradeValue string, feedbackText string, version int, formatter func(string) string) (string, string, error) {
	feedbackHash := hashTextSHA256Hex(feedbackText)
	doc := gradeCommitmentDocument{
		AssignmentID: assignmentID,
		StudentID:    studentID,
		SubmissionID: submissionID,
		GradeValue:   formatter(gradeValue),
		FeedbackHash: feedbackHash,
		Version:      version,
	}

	data, err := marshalCanonicalJSON(doc)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal grade commitment: %w", err)
	}

	return hashBytesSHA256Hex(data), feedbackHash, nil
}

func canonicalGradeValueForCommitment(raw string) string {
	trimmed := strings.TrimSpace(raw)
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return trimmed
	}

	return strconv.FormatFloat(value, 'f', 2, 64)
}

func legacyGradeValueForCommitment(raw string) string {
	trimmed := strings.TrimSpace(raw)
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return trimmed
	}

	return strconv.FormatFloat(value, 'f', -1, 64)
}

func normalizeGradeValue(raw string, maxRaw string) (float64, string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, "", ErrGradeValueRequired
	}

	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || value < 0 {
		return 0, "", ErrGradeValueInvalid
	}

	maxValue, err := strconv.ParseFloat(strings.TrimSpace(maxRaw), 64)
	if err == nil && value > maxValue {
		return 0, "", ErrGradeExceedsAssignmentMaximum
	}

	return value, canonicalGradeValueForCommitment(trimmed), nil
}

func nextGradeVersion(previousGrade *Grade) (int, *string, string) {
	if previousGrade == nil {
		return 1, nil, "grade_published"
	}

	return previousGrade.Version + 1, &previousGrade.ID, "grade_revised"
}

func hashTextSHA256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))

	return hex.EncodeToString(sum[:])
}
