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

func TestAssignmentHelpersRequireDatabase(t *testing.T) {
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
			name: "list assignments",
			call: func() error {
				_, err := ListAssignmentsForCourse(ctx, "course-id")
				return err
			},
		},
		{
			name: "get assignment",
			call: func() error {
				_, err := GetAssignmentByID(ctx, "assignment-id")
				return err
			},
		},
		{
			name: "create assignment",
			call: func() error {
				_, err := CreateAssignment(ctx, CreateAssignmentInput{})
				return err
			},
		},
		{
			name: "view assignment",
			call: func() error {
				_, err := CanUserViewAssignment(ctx, "user-id", "assignment-id")
				return err
			},
		},
		{
			name: "manage assignment",
			call: func() error {
				_, err := CanUserManageAssignment(ctx, "user-id", "assignment-id")
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

func TestComputeAssignmentMetadataHashStable(t *testing.T) {
	dueAt := time.Date(2026, time.April, 7, 14, 0, 0, 0, time.UTC)
	assignment := Assignment{
		ID:          "assignment-1",
		CourseID:    "course-1",
		Title:       "Assignment 1",
		Description: "Submit a design report",
		DueAt:       &dueAt,
		MaxGrade:    "100",
		CreatedBy:   "teacher-1",
		CreatedAt:   time.Date(2026, time.April, 5, 10, 0, 0, 0, time.UTC),
	}

	hashA, err := computeAssignmentMetadataHash(assignment)
	if err != nil {
		t.Fatalf("computeAssignmentMetadataHash() error = %v", err)
	}

	hashB, err := computeAssignmentMetadataHash(assignment)
	if err != nil {
		t.Fatalf("computeAssignmentMetadataHash() error = %v", err)
	}

	if hashA != hashB {
		t.Fatalf("expected stable metadata hash, got %q and %q", hashA, hashB)
	}

	assignment.Description = "Submit a revised design report"
	hashC, err := computeAssignmentMetadataHash(assignment)
	if err != nil {
		t.Fatalf("computeAssignmentMetadataHash() error = %v", err)
	}

	if hashC == hashA {
		t.Fatal("expected metadata hash to change when assignment metadata changes")
	}
}

func TestBuildAssignmentPublishedBlockchainEvent(t *testing.T) {
	dueAt := time.Date(2026, time.April, 7, 14, 0, 0, 0, time.UTC)
	assignment := Assignment{
		ID:           "assignment-1",
		CourseID:     "course-1",
		Title:        "Assignment 1",
		Description:  "Submit a design report",
		DueAt:        &dueAt,
		MaxGrade:     "100",
		CreatedBy:    "teacher-1",
		CreatedAt:    time.Date(2026, time.April, 5, 10, 0, 0, 0, time.UTC),
		MetadataHash: "abc123",
	}

	event := buildAssignmentPublishedBlockchainEvent(assignment)
	if event.EventType != "assignment_published" || event.EntityType != "assignment" || event.EntityID != assignment.ID {
		t.Fatalf("unexpected event envelope: %#v", event)
	}
	if event.ActorUserID != assignment.CreatedBy {
		t.Fatalf("unexpected actor user ID: %#v", event)
	}

	payload, ok := event.Data.(assignmentPublishedEventData)
	if !ok {
		t.Fatalf("expected assignmentPublishedEventData, got %T", event.Data)
	}
	if payload.MetadataHash != assignment.MetadataHash || payload.MaxGrade != assignment.MaxGrade {
		t.Fatalf("unexpected event payload: %#v", payload)
	}
}

func TestNormalizeAssignmentMaxGrade(t *testing.T) {
	if _, _, err := normalizeAssignmentMaxGrade(""); err != ErrAssignmentMaxGradeRequired {
		t.Fatalf("expected %v, got %v", ErrAssignmentMaxGradeRequired, err)
	}

	if _, _, err := normalizeAssignmentMaxGrade("-1"); err != ErrAssignmentMaxGradeInvalid {
		t.Fatalf("expected %v, got %v", ErrAssignmentMaxGradeInvalid, err)
	}

	if value, display, err := normalizeAssignmentMaxGrade("100.00"); err != nil {
		t.Fatalf("normalizeAssignmentMaxGrade() error = %v", err)
	} else if value != 100 || display != "100" {
		t.Fatalf("unexpected normalized assignment max grade: %v, %q", value, display)
	}
}
