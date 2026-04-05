/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package db

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestCertificateHelpersRequireDatabase(t *testing.T) {
	originalPool := pool
	pool = nil
	t.Cleanup(func() {
		pool = originalPool
	})

	ctx := context.Background()
	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "mark completion",
			call: func() error {
				_, err := MarkCourseCompletion(ctx, MarkCourseCompletionInput{})
				return err
			},
		},
		{
			name: "list completions",
			call: func() error {
				_, err := ListCourseCompletions(ctx, "course-id")
				return err
			},
		},
		{
			name: "issue certificate",
			call: func() error {
				_, err := IssueCertificate(ctx, IssueCertificateInput{})
				return err
			},
		},
		{
			name: "revoke certificate",
			call: func() error {
				_, err := RevokeCertificate(ctx, RevokeCertificateInput{})
				return err
			},
		},
		{
			name: "list certificates",
			call: func() error {
				_, err := ListCourseCertificates(ctx, "course-id")
				return err
			},
		},
		{
			name: "get certificate by code",
			call: func() error {
				_, err := GetCertificateByCode(ctx, "CERT-ABC")
				return err
			},
		},
		{
			name: "verify certificate claims",
			call: func() error {
				_, err := VerifyCertificateClaims(ctx, "CERT-ABC", "", "", "", "")
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, ErrDatabaseConnectionNotInitialized) {
				t.Fatalf("expected %v, got %v", ErrDatabaseConnectionNotInitialized, err)
			}
		})
	}
}

func TestComputeCertificateHashStable(t *testing.T) {
	doc := certificateHashDocument{
		CertificateCode:    "CERT-ABC123",
		CourseID:           "course-1",
		StudentID:          "student-1",
		StudentDisplayName: "Alice",
		CourseCode:         "CS8602",
		CourseTitle:        "Blockchain LMS",
		ResultSummary:      "completed",
		GradeSummary:       "95",
		IssuedAt:           time.Date(2026, time.April, 10, 9, 0, 0, 0, time.UTC),
	}

	hashA, err := computeCertificateHash(doc)
	if err != nil {
		t.Fatalf("computeCertificateHash() error = %v", err)
	}
	hashB, err := computeCertificateHash(doc)
	if err != nil {
		t.Fatalf("computeCertificateHash() error = %v", err)
	}
	if hashA != hashB {
		t.Fatalf("expected stable certificate hash, got %q and %q", hashA, hashB)
	}

	doc.GradeSummary = "96"
	hashC, err := computeCertificateHash(doc)
	if err != nil {
		t.Fatalf("computeCertificateHash() error = %v", err)
	}
	if hashA == hashC {
		t.Fatal("expected certificate hash to change when grade summary changes")
	}
}

func TestComputeCertificateHashNormalizesIssuedAt(t *testing.T) {
	base := certificateHashDocument{
		CertificateCode:    "CERT-ABC123",
		CourseID:           "course-1",
		StudentID:          "student-1",
		StudentDisplayName: "Alice",
		CourseCode:         "CS8602",
		CourseTitle:        "Blockchain LMS",
		ResultSummary:      "completed",
		GradeSummary:       "95",
		IssuedAt:           time.Date(2026, time.April, 10, 9, 0, 0, 123456789, time.UTC),
	}
	normalized := base
	normalized.IssuedAt = normalizeBlockchainTime(base.IssuedAt)

	hashA, err := computeCertificateHash(base)
	if err != nil {
		t.Fatalf("computeCertificateHash() error = %v", err)
	}
	hashB, err := computeCertificateHash(normalized)
	if err != nil {
		t.Fatalf("computeCertificateHash() error = %v", err)
	}
	if hashA != hashB {
		t.Fatalf("expected normalized certificate hash, got %q and %q", hashA, hashB)
	}
}

func TestBuildCertificateBlockchainEvents(t *testing.T) {
	issuedAt := time.Date(2026, time.April, 10, 9, 0, 0, 0, time.UTC)
	revokedAt := issuedAt.Add(time.Hour)
	issuedBy := "admin-1"
	revokedBy := "admin-2"
	completionID := "completion-1"
	certificate := CertificateRecord{
		ID:                 "certificate-1",
		CourseID:           "course-1",
		StudentID:          "student-1",
		CompletionID:       &completionID,
		CertificateCode:    "CERT-ABC123",
		CertificateHash:    "hash-1",
		StudentDisplayName: "Alice",
		CourseCode:         "CS8602",
		CourseTitle:        "Blockchain LMS",
		ResultSummary:      "completed",
		GradeSummary:       "95",
		IssuedBy:           &issuedBy,
		IssuedAt:           issuedAt,
		RevokedAt:          &revokedAt,
		RevokedBy:          &revokedBy,
	}

	issued := buildCertificateIssuedBlockchainEvent(certificate)
	if issued.EventType != "certificate_issued" || issued.EntityType != "certificate" || issued.EntityID != certificate.ID {
		t.Fatalf("unexpected issued event envelope: %#v", issued)
	}

	revoked := buildCertificateRevokedBlockchainEvent(certificate)
	if revoked.EventType != "certificate_revoked" || revoked.EntityID != certificate.ID {
		t.Fatalf("unexpected revoked event envelope: %#v", revoked)
	}
}

func TestMatchesLegacyCertificateIssuedRecord(t *testing.T) {
	issuedAt := time.Date(2026, time.April, 5, 19, 30, 10, 960321000, time.UTC)
	issuedBy := "98118fd5-e025-43cc-b1b5-434944bcef90"
	certificate := CertificateRecord{
		ID:                 "9e3ae412-0c45-468a-b9d4-edec444cb611",
		CourseID:           "e26d1118-4053-4aad-b5dc-b6469572a9c3",
		StudentID:          "768b7f78-54ff-4ae2-8eb9-06d9a2a03ffb",
		CertificateCode:    "CERT-FB05D5910BF8",
		CertificateHash:    "b0c83f5fdf2d02fd99cb0c29e323cc74a2a8b0f635b0621739c289f181f3999f",
		StudentDisplayName: "Student A",
		CourseCode:         "CS8602",
		CourseTitle:        "Something",
		ResultSummary:      "completed",
		GradeSummary:       "",
		IssuedBy:           &issuedBy,
		IssuedAt:           issuedAt,
	}

	payload := BlockchainEventPayload{
		EventType:   "certificate_issued",
		EntityType:  "certificate",
		EntityID:    certificate.ID,
		ActorUserID: issuedBy,
		OccurredAt:  formatBlockchainTime(issuedAt),
		Data:        json.RawMessage(`{"course_id":"e26d1118-4053-4aad-b5dc-b6469572a9c3","issued_by":"98118fd5-e025-43cc-b1b5-434944bcef90","student_id":"768b7f78-54ff-4ae2-8eb9-06d9a2a03ffb","course_code":"CS8602","course_title":"Something","grade_summary":"","certificate_id":"9e3ae412-0c45-468a-b9d4-edec444cb611","result_summary":"completed","certificate_code":"CERT-FB05D5910BF8","certificate_hash":"b0c83f5fdf2d02fd99cb0c29e323cc74a2a8b0f635b0621739c289f181f3999f","student_display_name":"Student A"}`),
	}
	payloadJSON, err := marshalCanonicalJSON(payload)
	if err != nil {
		t.Fatalf("marshalCanonicalJSON() error = %v", err)
	}

	record := BlockchainRecordSummary{
		EventType:   "certificate_issued",
		EntityType:  "certificate",
		EntityID:    stringPointer(certificate.ID),
		PayloadJSON: payloadJSON,
		OccurredAt:  issuedAt,
	}

	matches, err := matchesLegacyCertificateIssuedRecord(certificate, record)
	if err != nil {
		t.Fatalf("matchesLegacyCertificateIssuedRecord() error = %v", err)
	}
	if !matches {
		t.Fatal("expected legacy certificate blockchain record to match")
	}
}
