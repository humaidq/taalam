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

func TestSubmissionHelpersRequireDatabase(t *testing.T) {
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
			name: "create submission",
			call: func() error {
				_, err := CreateSubmission(ctx, CreateSubmissionInput{})
				return err
			},
		},
		{
			name: "latest submission",
			call: func() error {
				_, err := GetLatestSubmissionForStudentAssignment(ctx, "student-id", "assignment-id")
				return err
			},
		},
		{
			name: "submission receipt",
			call: func() error {
				_, err := GetSubmissionReceipt(ctx, "submission-id")
				return err
			},
		},
		{
			name: "submit permission",
			call: func() error {
				_, err := CanUserSubmitAssignment(ctx, "student-id", "assignment-id")
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

func TestHashSubmissionBytesSHA256Hex(t *testing.T) {
	first := hashSubmissionBytesSHA256Hex([]byte("hello"))
	second := hashSubmissionBytesSHA256Hex([]byte("hello"))
	third := hashSubmissionBytesSHA256Hex([]byte("hello world"))

	if first != second {
		t.Fatalf("expected stable hash, got %q and %q", first, second)
	}
	if first == third {
		t.Fatal("expected different file bytes to produce a different hash")
	}
}

func TestBuildSubmissionCommittedBlockchainEvent(t *testing.T) {
	submittedAt := time.Date(2026, time.April, 8, 9, 30, 0, 0, time.UTC)
	submission := Submission{
		ID:           "submission-1",
		AssignmentID: "assignment-1",
		StudentID:    "student-1",
		Version:      2,
		FileName:     "report.pdf",
		FileSize:     2048,
		FileSHA256:   "abc123",
		SubmittedAt:  submittedAt,
	}

	event := buildSubmissionCommittedBlockchainEvent(submission)
	if event.EventType != "submission_committed" || event.EntityType != "submission" || event.EntityID != submission.ID {
		t.Fatalf("unexpected event envelope: %#v", event)
	}

	payload, ok := event.Data.(submissionCommittedEventData)
	if !ok {
		t.Fatalf("expected submissionCommittedEventData, got %T", event.Data)
	}
	if payload.FileHash != submission.FileSHA256 || payload.Version != submission.Version {
		t.Fatalf("unexpected event payload: %#v", payload)
	}
}

func TestNextSubmissionVersionFromLatest(t *testing.T) {
	tests := []struct {
		name          string
		latestVersion int
		want          int
	}{
		{name: "no submissions yet", latestVersion: 0, want: 1},
		{name: "negative version coerces to first", latestVersion: -1, want: 1},
		{name: "increments latest version", latestVersion: 2, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nextSubmissionVersionFromLatest(tt.latestVersion); got != tt.want {
				t.Fatalf("nextSubmissionVersionFromLatest(%d) = %d, want %d", tt.latestVersion, got, tt.want)
			}
		})
	}
}
