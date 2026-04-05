/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CourseCompletion represents a course completion row with student metadata.
type CourseCompletion struct {
	ID                 string
	CourseID           string
	StudentID          string
	StudentDisplayName string
	StudentUsername    *string
	MarkedBy           *string
	CompletedAt        time.Time
	Status             string
	CertificateID      *string
	CertificateCode    *string
}

// CertificateRecord represents an issued certificate plus immutable snapshots.
type CertificateRecord struct {
	ID                 string
	CourseID           string
	StudentID          string
	CompletionID       *string
	CertificateCode    string
	CertificateHash    string
	StudentDisplayName string
	CourseCode         string
	CourseTitle        string
	ResultSummary      string
	GradeSummary       string
	IssuedBy           *string
	IssuedAt           time.Time
	RevokedAt          *time.Time
	RevokedBy          *string
}

// CertificateVerification summarizes public verification checks.
type CertificateVerification struct {
	Certificate      CertificateRecord
	ComputedHash     string
	HashMatches      bool
	LegacyCompatible bool
	StudentClaim     string
	StudentMatches   bool
	CourseClaim      string
	CourseMatches    bool
	ResultClaim      string
	ResultMatches    bool
	GradeClaim       string
	GradeMatches     bool
	Revoked          bool
}

// MarkCourseCompletionInput defines fields for marking completion.
type MarkCourseCompletionInput struct {
	CourseID  string
	StudentID string
	MarkedBy  string
}

// IssueCertificateInput defines fields for issuing a certificate.
type IssueCertificateInput struct {
	CourseID  string
	StudentID string
	IssuedBy  string
}

// RevokeCertificateInput defines fields for revoking a certificate.
type RevokeCertificateInput struct {
	CertificateID string
	RevokedBy     string
}

// MarkCourseCompletion upserts a completed status for a course/student pair.
func MarkCourseCompletion(ctx context.Context, input MarkCourseCompletionInput) (*CourseCompletion, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	courseID := strings.TrimSpace(input.CourseID)
	studentID := strings.TrimSpace(input.StudentID)
	markedBy := strings.TrimSpace(input.MarkedBy)
	if _, err := uuid.Parse(courseID); err != nil {
		return nil, ErrCourseNotFound
	}
	if _, err := uuid.Parse(studentID); err != nil {
		return nil, ErrUserNotFound
	}
	if _, err := uuid.Parse(markedBy); err != nil {
		return nil, ErrInvalidCreatorID
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin course completion transaction: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback course completion transaction", "error", rollbackErr)
		}
	}()

	if err := ensureCourseExistsTx(ctx, tx, courseID); err != nil {
		return nil, err
	}
	if err := requireUserRoleTx(ctx, tx, studentID, RoleStudent); err != nil {
		return nil, err
	}
	if err := requireStudentEnrolledTx(ctx, tx, studentID, courseID); err != nil {
		return nil, err
	}
	if err := requireCourseManagementAccessTx(ctx, tx, markedBy, courseID); err != nil {
		return nil, err
	}

	var completion CourseCompletion
	err = tx.QueryRow(ctx, `
		INSERT INTO course_completions (course_id, student_id, marked_by, status)
		VALUES ($1, $2, $3, 'completed')
		ON CONFLICT (course_id, student_id)
		DO UPDATE SET marked_by = EXCLUDED.marked_by, completed_at = NOW(), status = 'completed'
		RETURNING id, course_id, student_id, marked_by, completed_at, status
	`, courseID, studentID, markedBy).Scan(
		&completion.ID,
		&completion.CourseID,
		&completion.StudentID,
		&completion.MarkedBy,
		&completion.CompletedAt,
		&completion.Status,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to mark course completion: %w", err)
	}

	if err := loadCourseCompletionStudentMetadataTx(ctx, tx, &completion); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit course completion transaction: %w", err)
	}

	return &completion, nil
}

// ListCourseCompletions returns completions for a course.
func ListCourseCompletions(ctx context.Context, courseID string) ([]CourseCompletion, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	rows, err := pool.Query(ctx, `
		SELECT
			cc.id,
			cc.course_id,
			cc.student_id,
			u.display_name,
			u.username,
			cc.marked_by,
			cc.completed_at,
			cc.status,
			cr.id,
			cr.certificate_code
		FROM course_completions cc
		JOIN users u ON u.id = cc.student_id
		LEFT JOIN certificate_records cr
		  ON cr.completion_id = cc.id
		 AND cr.revoked_at IS NULL
		WHERE cc.course_id = $1
		ORDER BY cc.completed_at DESC, u.display_name ASC
	`, courseID)
	if err != nil {
		return nil, fmt.Errorf("failed to list course completions: %w", err)
	}
	defer rows.Close()

	completions := make([]CourseCompletion, 0)
	for rows.Next() {
		var completion CourseCompletion
		if err := rows.Scan(
			&completion.ID,
			&completion.CourseID,
			&completion.StudentID,
			&completion.StudentDisplayName,
			&completion.StudentUsername,
			&completion.MarkedBy,
			&completion.CompletedAt,
			&completion.Status,
			&completion.CertificateID,
			&completion.CertificateCode,
		); err != nil {
			return nil, fmt.Errorf("failed to scan course completion: %w", err)
		}

		completions = append(completions, completion)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating course completions: %w", err)
	}

	return completions, nil
}

// IssueCertificate creates a certificate record and emits a blockchain event.
func IssueCertificate(ctx context.Context, input IssueCertificateInput) (*CertificateRecord, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	courseID := strings.TrimSpace(input.CourseID)
	studentID := strings.TrimSpace(input.StudentID)
	issuedBy := strings.TrimSpace(input.IssuedBy)
	if _, err := uuid.Parse(courseID); err != nil {
		return nil, ErrCourseNotFound
	}
	if _, err := uuid.Parse(studentID); err != nil {
		return nil, ErrUserNotFound
	}
	if _, err := uuid.Parse(issuedBy); err != nil {
		return nil, ErrInvalidCreatorID
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin certificate transaction: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback certificate transaction", "error", rollbackErr)
		}
	}()

	if err := requireUserRoleTx(ctx, tx, issuedBy, RoleAdmin); err != nil {
		return nil, err
	}
	if err := ensureCourseExistsTx(ctx, tx, courseID); err != nil {
		return nil, err
	}
	completion, err := getCompletedCourseCompletionTx(ctx, tx, courseID, studentID)
	if err != nil {
		return nil, err
	}
	if err := acquireBlockchainLockTx(ctx, tx); err != nil {
		return nil, err
	}

	if activeCertificate, err := getActiveCertificateForCourseStudentTx(ctx, tx, courseID, studentID); err != nil {
		return nil, err
	} else if activeCertificate != nil {
		return nil, ErrCourseAlreadyCertified
	}

	gradeSummary, err := latestGradeSummaryForCourseStudentTx(ctx, tx, courseID, studentID)
	if err != nil {
		return nil, err
	}

	studentDisplayName, studentUsername, courseCode, courseTitle, err := loadCertificateSnapshotsTx(ctx, tx, courseID, studentID)
	if err != nil {
		return nil, err
	}

	certificateCode := generateCertificateCode()
	issuedAt := normalizeBlockchainTime(time.Now().UTC())
	certificateHash, err := computeCertificateHash(certificateHashDocument{
		CertificateCode:    certificateCode,
		CourseID:           courseID,
		StudentID:          studentID,
		StudentDisplayName: studentDisplayName,
		CourseCode:         courseCode,
		CourseTitle:        courseTitle,
		ResultSummary:      completion.Status,
		GradeSummary:       gradeSummary,
		IssuedAt:           issuedAt,
	})
	if err != nil {
		return nil, err
	}

	var certificate CertificateRecord
	err = tx.QueryRow(ctx, `
		INSERT INTO certificate_records (
			course_id,
			student_id,
			completion_id,
			certificate_code,
			certificate_hash,
			issued_by,
			student_display_name,
			course_code,
			course_title,
			result_summary,
			grade_summary,
			issued_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, course_id, student_id, completion_id, certificate_code, certificate_hash, student_display_name, course_code, course_title, result_summary, grade_summary, issued_by, issued_at, revoked_at, revoked_by
	`, courseID, studentID, completion.ID, certificateCode, certificateHash, issuedBy, studentDisplayName, courseCode, courseTitle, completion.Status, gradeSummary, issuedAt).Scan(
		&certificate.ID,
		&certificate.CourseID,
		&certificate.StudentID,
		&certificate.CompletionID,
		&certificate.CertificateCode,
		&certificate.CertificateHash,
		&certificate.StudentDisplayName,
		&certificate.CourseCode,
		&certificate.CourseTitle,
		&certificate.ResultSummary,
		&certificate.GradeSummary,
		&certificate.IssuedBy,
		&certificate.IssuedAt,
		&certificate.RevokedAt,
		&certificate.RevokedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to issue certificate: %w", err)
	}

	if _, err := appendBlockchainEventTx(ctx, tx, buildCertificateIssuedBlockchainEvent(certificate)); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit certificate transaction: %w", err)
	}

	_ = studentUsername

	return &certificate, nil
}

// RevokeCertificate marks a certificate revoked and emits a blockchain event.
func RevokeCertificate(ctx context.Context, input RevokeCertificateInput) (*CertificateRecord, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	certificateID := strings.TrimSpace(input.CertificateID)
	revokedBy := strings.TrimSpace(input.RevokedBy)
	if _, err := uuid.Parse(certificateID); err != nil {
		return nil, ErrCertificateNotFound
	}
	if _, err := uuid.Parse(revokedBy); err != nil {
		return nil, ErrInvalidCreatorID
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin certificate revocation transaction: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback certificate revocation transaction", "error", rollbackErr)
		}
	}()

	if err := requireUserRoleTx(ctx, tx, revokedBy, RoleAdmin); err != nil {
		return nil, err
	}
	if err := acquireBlockchainLockTx(ctx, tx); err != nil {
		return nil, err
	}

	var certificate CertificateRecord
	err = tx.QueryRow(ctx, `
		UPDATE certificate_records
		SET revoked_at = NOW(), revoked_by = $2
		WHERE id = $1
		  AND revoked_at IS NULL
		RETURNING id, course_id, student_id, completion_id, certificate_code, certificate_hash, student_display_name, course_code, course_title, result_summary, grade_summary, issued_by, issued_at, revoked_at, revoked_by
	`, certificateID, revokedBy).Scan(
		&certificate.ID,
		&certificate.CourseID,
		&certificate.StudentID,
		&certificate.CompletionID,
		&certificate.CertificateCode,
		&certificate.CertificateHash,
		&certificate.StudentDisplayName,
		&certificate.CourseCode,
		&certificate.CourseTitle,
		&certificate.ResultSummary,
		&certificate.GradeSummary,
		&certificate.IssuedBy,
		&certificate.IssuedAt,
		&certificate.RevokedAt,
		&certificate.RevokedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCertificateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to revoke certificate: %w", err)
	}

	if _, err := appendBlockchainEventTx(ctx, tx, buildCertificateRevokedBlockchainEvent(certificate)); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit certificate revocation transaction: %w", err)
	}

	return &certificate, nil
}

// ListCourseCertificates returns certificates for a course.
func ListCourseCertificates(ctx context.Context, courseID string) ([]CertificateRecord, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	rows, err := pool.Query(ctx, `
		SELECT id, course_id, student_id, completion_id, certificate_code, certificate_hash, student_display_name, course_code, course_title, result_summary, grade_summary, issued_by, issued_at, revoked_at, revoked_by
		FROM certificate_records
		WHERE course_id = $1
		ORDER BY issued_at DESC
	`, courseID)
	if err != nil {
		return nil, fmt.Errorf("failed to list course certificates: %w", err)
	}
	defer rows.Close()

	certificates := make([]CertificateRecord, 0)
	for rows.Next() {
		var certificate CertificateRecord
		if err := rows.Scan(
			&certificate.ID,
			&certificate.CourseID,
			&certificate.StudentID,
			&certificate.CompletionID,
			&certificate.CertificateCode,
			&certificate.CertificateHash,
			&certificate.StudentDisplayName,
			&certificate.CourseCode,
			&certificate.CourseTitle,
			&certificate.ResultSummary,
			&certificate.GradeSummary,
			&certificate.IssuedBy,
			&certificate.IssuedAt,
			&certificate.RevokedAt,
			&certificate.RevokedBy,
		); err != nil {
			return nil, fmt.Errorf("failed to scan certificate: %w", err)
		}

		certificates = append(certificates, certificate)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating certificates: %w", err)
	}

	return certificates, nil
}

// GetCertificateByCode returns a certificate by public code.
func GetCertificateByCode(ctx context.Context, code string) (*CertificateRecord, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	return getCertificateByCodeQuerier(ctx, pool, code)
}

// VerifyCertificateClaims verifies a certificate code and optional claims.
func VerifyCertificateClaims(ctx context.Context, code string, studentClaim string, courseClaim string, resultClaim string, gradeClaim string) (*CertificateVerification, error) {
	certificate, err := GetCertificateByCode(ctx, code)
	if err != nil {
		return nil, err
	}

	computedHash, err := computeCertificateHash(certificateHashDocument{
		CertificateCode:    certificate.CertificateCode,
		CourseID:           certificate.CourseID,
		StudentID:          certificate.StudentID,
		StudentDisplayName: certificate.StudentDisplayName,
		CourseCode:         certificate.CourseCode,
		CourseTitle:        certificate.CourseTitle,
		ResultSummary:      certificate.ResultSummary,
		GradeSummary:       certificate.GradeSummary,
		IssuedAt:           certificate.IssuedAt,
	})
	if err != nil {
		return nil, err
	}

	verification := &CertificateVerification{
		Certificate:      *certificate,
		ComputedHash:     computedHash,
		HashMatches:      computedHash == certificate.CertificateHash,
		LegacyCompatible: false,
		StudentClaim:     strings.TrimSpace(studentClaim),
		CourseClaim:      strings.TrimSpace(courseClaim),
		ResultClaim:      strings.TrimSpace(resultClaim),
		GradeClaim:       strings.TrimSpace(gradeClaim),
		Revoked:          certificate.RevokedAt != nil,
		StudentMatches:   true,
		CourseMatches:    true,
		ResultMatches:    true,
		GradeMatches:     true,
	}
	if !verification.HashMatches {
		legacyMatches, err := verifyLegacyCertificateSnapshot(ctx, *certificate)
		if err != nil {
			return nil, err
		}
		if legacyMatches {
			verification.HashMatches = true
			verification.LegacyCompatible = true
			verification.ComputedHash = certificate.CertificateHash
		}
	}

	if verification.StudentClaim != "" {
		verification.StudentMatches = strings.EqualFold(verification.StudentClaim, certificate.StudentDisplayName)
	}
	if verification.CourseClaim != "" {
		verification.CourseMatches = strings.EqualFold(verification.CourseClaim, certificate.CourseTitle) || strings.EqualFold(verification.CourseClaim, certificate.CourseCode)
	}
	if verification.ResultClaim != "" {
		verification.ResultMatches = strings.EqualFold(verification.ResultClaim, certificate.ResultSummary)
	}
	if verification.GradeClaim != "" {
		verification.GradeMatches = strings.EqualFold(verification.GradeClaim, certificate.GradeSummary)
	}

	return verification, nil
}

type certificateHashDocument struct {
	CertificateCode    string    `json:"certificate_code"`
	CourseID           string    `json:"course_id"`
	StudentID          string    `json:"student_id"`
	StudentDisplayName string    `json:"student_display_name"`
	CourseCode         string    `json:"course_code"`
	CourseTitle        string    `json:"course_title"`
	ResultSummary      string    `json:"result_summary"`
	GradeSummary       string    `json:"grade_summary"`
	IssuedAt           time.Time `json:"issued_at"`
}

type certificateIssuedEventData struct {
	CertificateID      string `json:"certificate_id"`
	CertificateCode    string `json:"certificate_code"`
	CertificateHash    string `json:"certificate_hash"`
	CourseID           string `json:"course_id"`
	StudentID          string `json:"student_id"`
	StudentDisplayName string `json:"student_display_name"`
	CourseCode         string `json:"course_code"`
	CourseTitle        string `json:"course_title"`
	ResultSummary      string `json:"result_summary"`
	GradeSummary       string `json:"grade_summary"`
	IssuedBy           string `json:"issued_by,omitempty"`
}

type certificateRevokedEventData struct {
	CertificateID   string `json:"certificate_id"`
	CertificateCode string `json:"certificate_code"`
	CourseID        string `json:"course_id"`
	StudentID       string `json:"student_id"`
	RevokedBy       string `json:"revoked_by,omitempty"`
	RevokedAt       string `json:"revoked_at"`
}

func buildCertificateIssuedBlockchainEvent(certificate CertificateRecord) AppendBlockchainEventInput {
	payload := certificateIssuedEventData{
		CertificateID:      certificate.ID,
		CertificateCode:    certificate.CertificateCode,
		CertificateHash:    certificate.CertificateHash,
		CourseID:           certificate.CourseID,
		StudentID:          certificate.StudentID,
		StudentDisplayName: certificate.StudentDisplayName,
		CourseCode:         certificate.CourseCode,
		CourseTitle:        certificate.CourseTitle,
		ResultSummary:      certificate.ResultSummary,
		GradeSummary:       certificate.GradeSummary,
	}
	actor := ""
	if certificate.IssuedBy != nil {
		actor = *certificate.IssuedBy
		payload.IssuedBy = actor
	}

	return AppendBlockchainEventInput{
		EventType:   "certificate_issued",
		EntityType:  "certificate",
		EntityID:    certificate.ID,
		ActorUserID: actor,
		OccurredAt:  certificate.IssuedAt,
		Data:        payload,
	}
}

func buildCertificateRevokedBlockchainEvent(certificate CertificateRecord) AppendBlockchainEventInput {
	payload := certificateRevokedEventData{
		CertificateID:   certificate.ID,
		CertificateCode: certificate.CertificateCode,
		CourseID:        certificate.CourseID,
		StudentID:       certificate.StudentID,
		RevokedAt:       formatBlockchainTime(pointerTimeValue(certificate.RevokedAt, time.Now().UTC())),
	}
	actor := ""
	if certificate.RevokedBy != nil {
		actor = *certificate.RevokedBy
		payload.RevokedBy = actor
	}

	return AppendBlockchainEventInput{
		EventType:   "certificate_revoked",
		EntityType:  "certificate",
		EntityID:    certificate.ID,
		ActorUserID: actor,
		OccurredAt:  pointerTimeValue(certificate.RevokedAt, time.Now().UTC()),
		Data:        payload,
	}
}

func computeCertificateHash(doc certificateHashDocument) (string, error) {
	issuedAt := normalizeBlockchainTime(doc.IssuedAt)

	data, err := marshalCanonicalJSON(struct {
		CertificateCode    string `json:"certificate_code"`
		CourseID           string `json:"course_id"`
		StudentID          string `json:"student_id"`
		StudentDisplayName string `json:"student_display_name"`
		CourseCode         string `json:"course_code"`
		CourseTitle        string `json:"course_title"`
		ResultSummary      string `json:"result_summary"`
		GradeSummary       string `json:"grade_summary"`
		IssuedAt           string `json:"issued_at"`
	}{
		CertificateCode:    doc.CertificateCode,
		CourseID:           doc.CourseID,
		StudentID:          doc.StudentID,
		StudentDisplayName: doc.StudentDisplayName,
		CourseCode:         doc.CourseCode,
		CourseTitle:        doc.CourseTitle,
		ResultSummary:      doc.ResultSummary,
		GradeSummary:       doc.GradeSummary,
		IssuedAt:           formatBlockchainTime(issuedAt),
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal certificate hash document: %w", err)
	}

	return hashBytesSHA256Hex(data), nil
}

func verifyLegacyCertificateSnapshot(ctx context.Context, certificate CertificateRecord) (bool, error) {
	records, err := ListBlockchainRecordsForEntity(ctx, "certificate", certificate.ID)
	if err != nil {
		return false, fmt.Errorf("failed to load certificate blockchain records: %w", err)
	}

	for _, record := range records {
		if record.EventType != "certificate_issued" {
			continue
		}

		matches, err := matchesLegacyCertificateIssuedRecord(certificate, record)
		if err != nil {
			return false, err
		}
		if matches {
			return true, nil
		}
	}

	return false, nil
}

func matchesLegacyCertificateIssuedRecord(certificate CertificateRecord, record BlockchainRecordSummary) (bool, error) {
	if record.EventType != "certificate_issued" || record.EntityType != "certificate" {
		return false, nil
	}
	if record.EntityID == nil || *record.EntityID != certificate.ID {
		return false, nil
	}

	payload, err := parseBlockchainPayloadJSON(record.PayloadJSON)
	if err != nil {
		return false, fmt.Errorf("failed to parse certificate blockchain payload: %w", err)
	}
	if payload.EventType != "certificate_issued" || payload.EntityType != "certificate" || payload.EntityID != certificate.ID {
		return false, nil
	}
	if payload.ActorUserID != stringValue(certificate.IssuedBy) {
		return false, nil
	}

	occurredAt, err := parseBlockchainTime(payload.OccurredAt)
	if err != nil {
		return false, fmt.Errorf("failed to parse certificate blockchain time: %w", err)
	}
	if !normalizeBlockchainTime(occurredAt).Equal(normalizeBlockchainTime(certificate.IssuedAt)) {
		return false, nil
	}
	if !normalizeBlockchainTime(record.OccurredAt).Equal(normalizeBlockchainTime(certificate.IssuedAt)) {
		return false, nil
	}

	var eventData certificateIssuedEventData
	if err := json.Unmarshal(payload.Data, &eventData); err != nil {
		return false, fmt.Errorf("failed to parse certificate blockchain event data: %w", err)
	}

	return eventData.CertificateID == certificate.ID &&
		eventData.CertificateCode == certificate.CertificateCode &&
		eventData.CertificateHash == certificate.CertificateHash &&
		eventData.CourseID == certificate.CourseID &&
		eventData.StudentID == certificate.StudentID &&
		eventData.StudentDisplayName == certificate.StudentDisplayName &&
		eventData.CourseCode == certificate.CourseCode &&
		eventData.CourseTitle == certificate.CourseTitle &&
		eventData.ResultSummary == certificate.ResultSummary &&
		eventData.GradeSummary == certificate.GradeSummary &&
		eventData.IssuedBy == stringValue(certificate.IssuedBy), nil
}

func generateCertificateCode() string {
	id := strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))
	if len(id) > 12 {
		id = id[:12]
	}

	return "CERT-" + id
}

func getCompletedCourseCompletionTx(ctx context.Context, tx pgx.Tx, courseID string, studentID string) (*CourseCompletion, error) {
	var completion CourseCompletion
	err := tx.QueryRow(ctx, `
		SELECT id, course_id, student_id, marked_by, completed_at, status
		FROM course_completions
		WHERE course_id = $1
		  AND student_id = $2
		  AND status = 'completed'
	`, courseID, studentID).Scan(
		&completion.ID,
		&completion.CourseID,
		&completion.StudentID,
		&completion.MarkedBy,
		&completion.CompletedAt,
		&completion.Status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCourseCompletionRequired
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load course completion: %w", err)
	}

	if err := loadCourseCompletionStudentMetadataTx(ctx, tx, &completion); err != nil {
		return nil, err
	}

	return &completion, nil
}

func loadCourseCompletionStudentMetadataTx(ctx context.Context, tx pgx.Tx, completion *CourseCompletion) error {
	if err := tx.QueryRow(ctx, `SELECT display_name, username FROM users WHERE id = $1`, completion.StudentID).Scan(&completion.StudentDisplayName, &completion.StudentUsername); err != nil {
		return fmt.Errorf("failed to load course completion student metadata: %w", err)
	}

	return nil
}

func getActiveCertificateForCourseStudentTx(ctx context.Context, tx pgx.Tx, courseID string, studentID string) (*CertificateRecord, error) {
	var certificate CertificateRecord
	err := tx.QueryRow(ctx, `
		SELECT id, course_id, student_id, completion_id, certificate_code, certificate_hash, student_display_name, course_code, course_title, result_summary, grade_summary, issued_by, issued_at, revoked_at, revoked_by
		FROM certificate_records
		WHERE course_id = $1
		  AND student_id = $2
		  AND revoked_at IS NULL
		LIMIT 1
	`, courseID, studentID).Scan(
		&certificate.ID,
		&certificate.CourseID,
		&certificate.StudentID,
		&certificate.CompletionID,
		&certificate.CertificateCode,
		&certificate.CertificateHash,
		&certificate.StudentDisplayName,
		&certificate.CourseCode,
		&certificate.CourseTitle,
		&certificate.ResultSummary,
		&certificate.GradeSummary,
		&certificate.IssuedBy,
		&certificate.IssuedAt,
		&certificate.RevokedAt,
		&certificate.RevokedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load active certificate: %w", err)
	}

	return &certificate, nil
}

func latestGradeSummaryForCourseStudentTx(ctx context.Context, tx pgx.Tx, courseID string, studentID string) (string, error) {
	var gradeSummary string
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(g.grade_value::text, '')
		FROM grades g
		JOIN assignments a ON a.id = g.assignment_id
		WHERE a.course_id = $1
		  AND g.student_id = $2
		ORDER BY g.published_at DESC, g.version DESC
		LIMIT 1
	`, courseID, studentID).Scan(&gradeSummary)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to load latest grade summary: %w", err)
	}

	return gradeSummary, nil
}

func loadCertificateSnapshotsTx(ctx context.Context, tx pgx.Tx, courseID string, studentID string) (string, *string, string, string, error) {
	var studentDisplayName string
	var studentUsername *string
	if err := tx.QueryRow(ctx, `SELECT display_name, username FROM users WHERE id = $1`, studentID).Scan(&studentDisplayName, &studentUsername); err != nil {
		return "", nil, "", "", fmt.Errorf("failed to load certificate student snapshot: %w", err)
	}

	var courseCode string
	var courseTitle string
	if err := tx.QueryRow(ctx, `SELECT code, title FROM courses WHERE id = $1`, courseID).Scan(&courseCode, &courseTitle); err != nil {
		return "", nil, "", "", fmt.Errorf("failed to load certificate course snapshot: %w", err)
	}

	return studentDisplayName, studentUsername, courseCode, courseTitle, nil
}

func getCertificateByCodeQuerier(ctx context.Context, querier blockchainHeadQuerier, code string) (*CertificateRecord, error) {
	var certificate CertificateRecord
	err := querier.QueryRow(ctx, `
		SELECT id, course_id, student_id, completion_id, certificate_code, certificate_hash, student_display_name, course_code, course_title, result_summary, grade_summary, issued_by, issued_at, revoked_at, revoked_by
		FROM certificate_records
		WHERE certificate_code = $1
		LIMIT 1
	`, strings.TrimSpace(code)).Scan(
		&certificate.ID,
		&certificate.CourseID,
		&certificate.StudentID,
		&certificate.CompletionID,
		&certificate.CertificateCode,
		&certificate.CertificateHash,
		&certificate.StudentDisplayName,
		&certificate.CourseCode,
		&certificate.CourseTitle,
		&certificate.ResultSummary,
		&certificate.GradeSummary,
		&certificate.IssuedBy,
		&certificate.IssuedAt,
		&certificate.RevokedAt,
		&certificate.RevokedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCertificateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get certificate by code: %w", err)
	}

	return &certificate, nil
}

func pointerTimeValue(value *time.Time, fallback time.Time) time.Time {
	if value == nil {
		return fallback
	}

	return value.UTC()
}
