/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGradeHelpersRequireDatabase(t *testing.T) {
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
			name: "publish grade",
			call: func() error {
				_, err := PublishGrade(ctx, PublishGradeInput{})
				return err
			},
		},
		{
			name: "latest grade",
			call: func() error {
				_, err := GetLatestGradeForAssignmentStudent(ctx, "assignment-id", "student-id")
				return err
			},
		},
		{
			name: "grade verification",
			call: func() error {
				_, err := GetGradeVerificationForAssignmentStudent(ctx, "assignment-id", "student-id")
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

func TestComputeGradeCommitmentStable(t *testing.T) {
	hashA, feedbackHashA, err := computeGradeCommitment("assignment-1", "student-1", "submission-1", "95", "Strong work", 1)
	if err != nil {
		t.Fatalf("computeGradeCommitment() error = %v", err)
	}
	hashB, feedbackHashB, err := computeGradeCommitment("assignment-1", "student-1", "submission-1", "95", "Strong work", 1)
	if err != nil {
		t.Fatalf("computeGradeCommitment() error = %v", err)
	}
	if hashA != hashB || feedbackHashA != feedbackHashB {
		t.Fatal("expected grade commitment and feedback hash to be stable")
	}

	hashC, _, err := computeGradeCommitment("assignment-1", "student-1", "submission-1", "96", "Strong work", 1)
	if err != nil {
		t.Fatalf("computeGradeCommitment() error = %v", err)
	}
	if hashA == hashC {
		t.Fatal("expected grade commitment to change when grade value changes")
	}
}

func TestComputeGradeCommitmentCanonicalizesNumericFormatting(t *testing.T) {
	hashA, _, err := computeGradeCommitment("assignment-1", "student-1", "submission-1", "95", "Strong work", 1)
	if err != nil {
		t.Fatalf("computeGradeCommitment() error = %v", err)
	}
	hashB, _, err := computeGradeCommitment("assignment-1", "student-1", "submission-1", "95.00", "Strong work", 1)
	if err != nil {
		t.Fatalf("computeGradeCommitment() error = %v", err)
	}
	if hashA != hashB {
		t.Fatalf("expected equivalent numeric grade formats to hash the same, got %q and %q", hashA, hashB)
	}
}

func TestComputeLegacyGradeCommitmentMatchesHistoricalFormatting(t *testing.T) {
	currentHash, _, err := computeGradeCommitment("assignment-1", "student-1", "submission-1", "95.00", "Strong work", 1)
	if err != nil {
		t.Fatalf("computeGradeCommitment() error = %v", err)
	}
	legacyHash, _, err := computeLegacyGradeCommitment("assignment-1", "student-1", "submission-1", "95.00", "Strong work", 1)
	if err != nil {
		t.Fatalf("computeLegacyGradeCommitment() error = %v", err)
	}
	originalHash, _, err := computeLegacyGradeCommitment("assignment-1", "student-1", "submission-1", "95", "Strong work", 1)
	if err != nil {
		t.Fatalf("computeLegacyGradeCommitment() error = %v", err)
	}
	if legacyHash != originalHash {
		t.Fatalf("expected legacy formatting hash %q, got %q", originalHash, legacyHash)
	}
	if legacyHash == currentHash {
		t.Fatal("expected legacy and canonical commitment hashes to differ for historical grade values")
	}
}

func TestBuildGradeBlockchainEvent(t *testing.T) {
	publishedAt := time.Date(2026, time.April, 9, 11, 0, 0, 0, time.UTC)
	previousGradeID := "grade-0"
	grade := Grade{
		ID:              "grade-1",
		AssignmentID:    "assignment-1",
		StudentID:       "student-1",
		SubmissionID:    "submission-1",
		Version:         2,
		GradeValue:      "95",
		CommitmentHash:  "commitment-1",
		PublishedBy:     "teacher-1",
		PublishedAt:     publishedAt,
		PreviousGradeID: &previousGradeID,
	}

	event := buildGradeBlockchainEvent("grade_revised", grade, "feedback-hash")
	if event.EventType != "grade_revised" || event.EntityType != "grade" || event.EntityID != grade.ID {
		t.Fatalf("unexpected event envelope: %#v", event)
	}

	payload, ok := event.Data.(gradeBlockchainEventData)
	if !ok {
		t.Fatalf("expected gradeBlockchainEventData, got %T", event.Data)
	}
	if payload.PreviousGradeID != previousGradeID || payload.CommitmentHash != grade.CommitmentHash {
		t.Fatalf("unexpected event payload: %#v", payload)
	}
}

func TestNextGradeVersion(t *testing.T) {
	version, previousGradeID, eventType := nextGradeVersion(nil)
	if version != 1 || previousGradeID != nil || eventType != "grade_published" {
		t.Fatalf("unexpected initial grade version tuple: %d, %v, %q", version, previousGradeID, eventType)
	}

	grade := &Grade{ID: "grade-1", Version: 2}
	version, previousGradeID, eventType = nextGradeVersion(grade)
	if version != 3 || previousGradeID == nil || *previousGradeID != grade.ID || eventType != "grade_revised" {
		t.Fatalf("unexpected revised grade version tuple: %d, %v, %q", version, previousGradeID, eventType)
	}
}

func TestNormalizeGradeValue(t *testing.T) {
	if _, _, err := normalizeGradeValue("", "100"); err != ErrGradeValueRequired {
		t.Fatalf("expected %v, got %v", ErrGradeValueRequired, err)
	}

	if _, _, err := normalizeGradeValue("abc", "100"); err != ErrGradeValueInvalid {
		t.Fatalf("expected %v, got %v", ErrGradeValueInvalid, err)
	}

	if _, _, err := normalizeGradeValue("101", "100"); err != ErrGradeExceedsAssignmentMaximum {
		t.Fatalf("expected %v, got %v", ErrGradeExceedsAssignmentMaximum, err)
	}

	value, display, err := normalizeGradeValue("95.50", "100")
	if err != nil {
		t.Fatalf("normalizeGradeValue() error = %v", err)
	}
	if value != 95.5 || display != "95.50" {
		t.Fatalf("unexpected normalized grade value: %v, %q", value, display)
	}
}
