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

func TestCoursePermissionHelpersRequireDatabase(t *testing.T) {
	originalPool := pool
	pool = nil
	t.Cleanup(func() {
		pool = originalPool
	})

	ctx := context.Background()
	tests := []struct {
		name string
		call func() (bool, error)
	}{
		{
			name: "course instructor",
			call: func() (bool, error) {
				return IsUserCourseInstructor(ctx, "user-id", "course-id")
			},
		},
		{
			name: "course student",
			call: func() (bool, error) {
				return IsUserCourseStudent(ctx, "user-id", "course-id")
			},
		},
		{
			name: "view course",
			call: func() (bool, error) {
				return CanUserViewCourse(ctx, "user-id", "course-id")
			},
		},
		{
			name: "manage course",
			call: func() (bool, error) {
				return CanUserManageCourse(ctx, "user-id", "course-id")
			},
		},
		{
			name: "view submission",
			call: func() (bool, error) {
				return CanUserViewSubmission(ctx, "user-id", "submission-id")
			},
		},
		{
			name: "view grade",
			call: func() (bool, error) {
				return CanUserViewGrade(ctx, "user-id", "grade-id")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, err := tt.call()
			if allowed {
				t.Fatalf("expected helper to deny access when database is unavailable")
			}

			if !errors.Is(err, ErrDatabaseConnectionNotInitialized) {
				t.Fatalf("expected %v, got %v", ErrDatabaseConnectionNotInitialized, err)
			}
		})
	}
}

func TestCourseWriteHelpersRequireDatabase(t *testing.T) {
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
			name: "create course",
			call: func() error {
				_, err := CreateCourse(ctx, CreateCourseInput{})
				return err
			},
		},
		{
			name: "assign instructor",
			call: func() error {
				return AssignCourseInstructor(ctx, AssignCourseInstructorInput{})
			},
		},
		{
			name: "enroll student",
			call: func() error {
				return EnrollCourseStudent(ctx, EnrollCourseStudentInput{})
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

func TestCourseBlockchainEventBuilders(t *testing.T) {
	course := Course{
		ID:        "course-1",
		Code:      "CS8602",
		Title:     "Blockchain LMS",
		Term:      "Spring 2026",
		CreatedBy: "admin-1",
		CreatedAt: time.Date(2026, time.April, 5, 10, 30, 0, 0, time.UTC),
	}

	created := buildCourseCreatedBlockchainEvent(course)
	if created.EventType != "course_created" || created.EntityType != "course" || created.EntityID != course.ID {
		t.Fatalf("unexpected course created event envelope: %#v", created)
	}
	createdData, ok := created.Data.(courseCreatedEventData)
	if !ok {
		t.Fatalf("expected courseCreatedEventData, got %T", created.Data)
	}
	if createdData.Code != course.Code || createdData.Title != course.Title || createdData.Term != course.Term {
		t.Fatalf("unexpected course created payload: %#v", createdData)
	}

	assigned := buildCourseInstructorAssignedBlockchainEvent("course-1", "teacher-1", "admin-1")
	if assigned.EventType != "instructor_assigned" || assigned.EntityID != "course-1" || assigned.ActorUserID != "admin-1" {
		t.Fatalf("unexpected instructor assigned event envelope: %#v", assigned)
	}
	assignedData, ok := assigned.Data.(courseInstructorAssignedEventData)
	if !ok {
		t.Fatalf("expected courseInstructorAssignedEventData, got %T", assigned.Data)
	}
	if assignedData.TeacherID != "teacher-1" {
		t.Fatalf("unexpected instructor assigned payload: %#v", assignedData)
	}
	if assigned.OccurredAt.IsZero() {
		t.Fatal("expected instructor assigned event timestamp")
	}

	enrolled := buildCourseStudentEnrolledBlockchainEvent("course-1", "student-1", "admin-1")
	if enrolled.EventType != "student_enrolled" || enrolled.EntityID != "course-1" || enrolled.ActorUserID != "admin-1" {
		t.Fatalf("unexpected student enrolled event envelope: %#v", enrolled)
	}
	enrolledData, ok := enrolled.Data.(courseStudentEnrolledEventData)
	if !ok {
		t.Fatalf("expected courseStudentEnrolledEventData, got %T", enrolled.Data)
	}
	if enrolledData.StudentID != "student-1" {
		t.Fatalf("unexpected student enrolled payload: %#v", enrolledData)
	}
	if enrolled.OccurredAt.IsZero() {
		t.Fatal("expected student enrolled event timestamp")
	}
}
