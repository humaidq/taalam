/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package routes

import (
	"testing"

	"github.com/humaidq/taalam/db"
)

func TestRequiredRoleMessage(t *testing.T) {
	tests := []struct {
		name     string
		required db.UserRole
		want     string
	}{
		{name: "admin", required: db.RoleAdmin, want: "Admin access required"},
		{name: "teacher", required: db.RoleTeacher, want: "Teacher access required"},
		{name: "student", required: db.RoleStudent, want: "Student access required"},
		{name: "unknown", required: db.UserRole("guest"), want: "Access restricted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := requiredRoleMessage(tt.required); got != tt.want {
				t.Fatalf("requiredRoleMessage(%q) = %q, want %q", tt.required, got, tt.want)
			}
		})
	}
}

func TestHumanizeWriteErrors(t *testing.T) {
	if got := humanizeSubmissionWriteError(db.ErrAccessDenied); got != "You can only submit to assignments for courses you are enrolled in" {
		t.Fatalf("unexpected submission error message: %q", got)
	}

	if got := humanizeGradeWriteError(db.ErrAccessDenied); got != "You can only grade assignments for courses you manage" {
		t.Fatalf("unexpected grade error message: %q", got)
	}

	if got := humanizeCertificateWriteError(db.ErrCourseCompletionRequired); got != db.ErrCourseCompletionRequired.Error() {
		t.Fatalf("unexpected certificate error message: %q", got)
	}
}
