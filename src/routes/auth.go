/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package routes

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/flamego/flamego"
	"github.com/flamego/session"
	"github.com/flamego/template"

	"github.com/humaidq/taalam/db"
)

// LoginForm renders the login page.
func LoginForm(c flamego.Context, s session.Session, t template.Template, data template.Data) {
	next := sanitizeNextPath(c.Query("next"))
	if strings.TrimSpace(c.Query("next")) == "" {
		next = "/"
	}

	if isSessionAuthenticated(s, time.Now()) {
		c.Redirect(next, http.StatusSeeOther)

		return
	}

	renderLoginPage(t, data, next, "", true, http.StatusOK, "")
}

// Login authenticates a user with username and password.
func Login(c flamego.Context, s session.Session, t template.Template, data template.Data) {
	if err := c.Request().ParseForm(); err != nil {
		renderLoginPage(t, data, "/", "", true, http.StatusBadRequest, "Failed to parse login form")

		return
	}

	next := sanitizeNextPath(c.Request().Form.Get("next"))
	if next == "/" && strings.TrimSpace(c.Request().Form.Get("next")) == "" {
		next = sanitizeNextPath(c.Query("next"))
	}
	if next == "/" && strings.TrimSpace(c.Query("next")) == "" && strings.TrimSpace(c.Request().Form.Get("next")) == "" {
		next = "/"
	}

	username := strings.TrimSpace(c.Request().Form.Get("username"))
	password := c.Request().Form.Get("password")
	remember := shouldRememberLogin(c.Request().Form.Get("remember"))

	user, err := db.AuthenticateUserWithPassword(c.Request().Context(), username, password)
	if err != nil {
		message := "Failed to sign in"
		status := http.StatusInternalServerError
		if errors.Is(err, db.ErrInvalidUsernameOrPassword) {
			message = db.ErrInvalidUsernameOrPassword.Error()
			status = http.StatusUnauthorized
		}

		renderLoginPage(t, data, next, username, remember, status, message)

		return
	}

	setAuthenticatedSession(s, user, time.Now(), remember)
	c.Redirect(next, http.StatusSeeOther)
}

// Logout clears authenticated state for the current session.
func Logout(s session.Session, c flamego.Context) {
	clearAuthenticatedSession(s)
	c.Redirect("/login", http.StatusSeeOther)
}

// RequireAuth blocks unauthenticated access.
func RequireAuth(s session.Session, c flamego.Context) {
	if !isSessionAuthenticated(s, time.Now()) {
		next := sanitizeNextPath(c.Request().Header.Get("Referer"))
		if c.Request().Method == http.MethodGet || c.Request().Method == http.MethodHead {
			next = sanitizeNextPath(c.Request().URL.RequestURI())
		}

		redirectURL := "/login?next=" + url.QueryEscape(next)
		c.Redirect(redirectURL, http.StatusFound)

		return
	}

	c.Next()
}

func sanitizeNextPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/"
	}

	if strings.Contains(raw, "\n") || strings.Contains(raw, "\r") {
		return "/"
	}

	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "/"
		}

		path := parsed.EscapedPath()
		if path == "" {
			path = "/"
		}

		if strings.HasPrefix(path, "//") {
			return "/"
		}

		if parsed.RawQuery != "" {
			return path + "?" + parsed.RawQuery
		}

		return path
	}

	if !strings.HasPrefix(raw, "/") {
		return "/"
	}

	if strings.HasPrefix(raw, "//") {
		return "/"
	}

	return raw
}

func renderLoginPage(t template.Template, data template.Data, next string, username string, remember bool, status int, errMessage string) {
	setPage(data, "Login")
	data["HeaderOnly"] = true
	data["Next"] = next
	data["Username"] = username
	data["RememberLogin"] = remember
	if errMessage != "" {
		data["Error"] = errMessage
	}

	t.HTML(status, "login")
}
