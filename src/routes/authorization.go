/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package routes

import (
	"net/http"
	"net/url"
	"time"

	"github.com/flamego/flamego"
	"github.com/flamego/session"

	"github.com/humaidq/taalam/db"
)

// RequireAdmin blocks access unless the current user is an admin.
func RequireAdmin(s session.Session, c flamego.Context) {
	requireRole(s, c, db.RoleAdmin)
}

// RequireTeacher blocks access unless the current user is a teacher or admin.
func RequireTeacher(s session.Session, c flamego.Context) {
	requireRole(s, c, db.RoleTeacher)
}

// RequireStudent blocks access unless the current user is a student or admin.
func RequireStudent(s session.Session, c flamego.Context) {
	requireRole(s, c, db.RoleStudent)
}

func requireRole(s session.Session, c flamego.Context, required db.UserRole) {
	if !isSessionAuthenticated(s, time.Now()) {
		next := sanitizeNextPath(c.Request().Header.Get("Referer"))
		if c.Request().Method == http.MethodGet || c.Request().Method == http.MethodHead {
			next = sanitizeNextPath(c.Request().URL.RequestURI())
		}

		c.Redirect("/login?next="+url.QueryEscape(next), http.StatusFound)

		return
	}

	role, err := resolveSessionRole(c.Request().Context(), s)
	if err != nil {
		logger.Error("failed to resolve user role", "error", err)
		SetErrorFlash(s, requiredRoleMessage(required))
		c.Redirect("/", http.StatusSeeOther)

		return
	}

	if !role.Satisfies(required) {
		SetErrorFlash(s, requiredRoleMessage(required))
		c.Redirect("/", http.StatusSeeOther)

		return
	}

	c.Next()
}

func requiredRoleMessage(required db.UserRole) string {
	switch required {
	case db.RoleAdmin:
		return "Admin access required"
	case db.RoleTeacher:
		return "Teacher access required"
	case db.RoleStudent:
		return "Student access required"
	default:
		return "Access restricted"
	}
}
