/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package db

import "testing"

func TestUserRoleHelpers(t *testing.T) {
	tests := []struct {
		name      string
		role      UserRole
		required  UserRole
		isAdmin   bool
		isTeacher bool
		isStudent bool
		satisfies bool
	}{
		{
			name:      "admin satisfies admin",
			role:      RoleAdmin,
			required:  RoleAdmin,
			isAdmin:   true,
			satisfies: true,
		},
		{
			name:      "admin satisfies teacher",
			role:      RoleAdmin,
			required:  RoleTeacher,
			isAdmin:   true,
			satisfies: true,
		},
		{
			name:      "admin satisfies student",
			role:      RoleAdmin,
			required:  RoleStudent,
			isAdmin:   true,
			satisfies: true,
		},
		{
			name:      "teacher satisfies teacher",
			role:      RoleTeacher,
			required:  RoleTeacher,
			isTeacher: true,
			satisfies: true,
		},
		{
			name:      "teacher does not satisfy student",
			role:      RoleTeacher,
			required:  RoleStudent,
			isTeacher: true,
			satisfies: false,
		},
		{
			name:      "student satisfies student",
			role:      RoleStudent,
			required:  RoleStudent,
			isStudent: true,
			satisfies: true,
		},
		{
			name:      "student does not satisfy teacher",
			role:      RoleStudent,
			required:  RoleTeacher,
			isStudent: true,
			satisfies: false,
		},
		{
			name:      "invalid role never satisfies",
			role:      UserRole("guest"),
			required:  RoleStudent,
			satisfies: false,
		},
		{
			name:      "invalid required role fails",
			role:      RoleTeacher,
			required:  UserRole("guest"),
			isTeacher: true,
			satisfies: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.role.IsAdmin(); got != tt.isAdmin {
				t.Fatalf("IsAdmin() = %v, want %v", got, tt.isAdmin)
			}

			if got := tt.role.IsTeacher(); got != tt.isTeacher {
				t.Fatalf("IsTeacher() = %v, want %v", got, tt.isTeacher)
			}

			if got := tt.role.IsStudent(); got != tt.isStudent {
				t.Fatalf("IsStudent() = %v, want %v", got, tt.isStudent)
			}

			if got := tt.role.Satisfies(tt.required); got != tt.satisfies {
				t.Fatalf("Satisfies(%q) = %v, want %v", tt.required, got, tt.satisfies)
			}
		})
	}
}
