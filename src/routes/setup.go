/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package routes

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/flamego/flamego"
	"github.com/flamego/session"
	"github.com/flamego/template"
	"github.com/google/uuid"

	"github.com/humaidq/taalam/db"
)

type setupPageState struct {
	Action        string
	DisplayName   string
	InviteID      string
	IsInviteSetup bool
	Role          db.UserRole
	RoleLabel     string
}

// SetupSubmit completes bootstrap or invite-based account setup using username/password.
func SetupSubmit(c flamego.Context, s session.Session, t template.Template, data template.Data) {
	if isSessionAuthenticated(s, time.Now()) {
		c.Redirect("/", http.StatusSeeOther)

		return
	}

	state, status, errMessage := loadSetupPageState(c, s)
	if state == nil {
		setPage(data, "Setup")
		data["HeaderOnly"] = true
		data["Error"] = errMessage
		t.HTML(status, "setup")

		return
	}

	if err := c.Request().ParseForm(); err != nil {
		renderSetupPage(t, data, state, http.StatusBadRequest, "Failed to parse form")

		return
	}

	displayName := strings.TrimSpace(c.Request().Form.Get("display_name"))
	username := strings.TrimSpace(c.Request().Form.Get("username"))
	password := c.Request().Form.Get("password")
	confirmPassword := c.Request().Form.Get("confirm_password")

	data["DisplayName"] = displayName
	data["SetupUsername"] = username

	if password != confirmPassword {
		renderSetupPage(t, data, state, http.StatusBadRequest, "Passwords do not match")

		return
	}

	var inviteID *string
	if state.InviteID != "" {
		inviteID = &state.InviteID
	}

	createdUser, err := db.FinalizeSetupPasswordAccount(c.Request().Context(), db.FinalizeSetupPasswordInput{
		UserID:      uuid.New(),
		DisplayName: displayName,
		Username:    username,
		Password:    password,
		Role:        state.Role,
		InviteID:    inviteID,
	})
	if err != nil {
		statusCode := http.StatusBadRequest
		message := humanizeSetupError(err)
		if message == "Failed to finalize setup" {
			statusCode = http.StatusInternalServerError
		}

		renderSetupPage(t, data, state, statusCode, message)

		return
	}

	setAuthenticatedSession(s, createdUser, time.Now(), true)
	clearSetupSession(s)

	c.Redirect("/", http.StatusSeeOther)
}

func renderSetupPage(t template.Template, data template.Data, state *setupPageState, status int, errMessage string) {
	setPage(data, "Setup")
	data["HeaderOnly"] = true
	data["BootstrapReady"] = true
	data["IsInviteSetup"] = state.IsInviteSetup
	data["Role"] = string(state.Role)
	data["RoleLabel"] = state.RoleLabel
	data["SetupAction"] = state.Action
	if _, ok := data["DisplayName"]; !ok {
		data["DisplayName"] = state.DisplayName
	}
	if errMessage != "" {
		data["Error"] = errMessage
	}

	t.HTML(status, "setup")
}

func loadSetupPageState(c flamego.Context, s session.Session) (*setupPageState, int, string) {
	s.Delete(webauthnInviteAllowedKey)
	s.Delete(webauthnInviteIDKey)

	ctx := c.Request().Context()
	count, err := db.CountUsers(ctx)
	if err != nil {
		return nil, http.StatusInternalServerError, "Failed to load authentication state"
	}

	token := strings.TrimSpace(c.Query("token"))
	action := c.Request().URL.RequestURI()
	if action == "" {
		action = "/setup"
	}

	if count == 0 {
		bootstrapToken := strings.TrimSpace(os.Getenv("BOOTSTRAP_TOKEN"))
		if bootstrapToken == "" {
			s.Delete(webauthnBootstrapAllowedKey)

			return nil, http.StatusForbidden, "Setup is unavailable"
		}

		if token == "" || token != bootstrapToken {
			s.Delete(webauthnBootstrapAllowedKey)

			return nil, http.StatusForbidden, "Invalid setup link"
		}

		s.Set(webauthnBootstrapAllowedKey, true)

		return &setupPageState{
			Action:        action,
			DisplayName:   "Admin",
			IsInviteSetup: false,
			Role:          db.RoleAdmin,
			RoleLabel:     db.RoleAdmin.Label(),
		}, http.StatusOK, ""
	}

	s.Delete(webauthnBootstrapAllowedKey)

	if token == "" {
		return nil, http.StatusForbidden, "Invalid setup link"
	}

	invite, err := db.GetUserInviteByToken(ctx, token)
	if err != nil {
		return nil, http.StatusInternalServerError, "Failed to load setup link"
	}

	if invite == nil || invite.UsedAt != nil {
		s.Delete(webauthnInviteAllowedKey)
		s.Delete(webauthnInviteIDKey)

		return nil, http.StatusForbidden, "Invalid setup link"
	}

	s.Set(webauthnInviteAllowedKey, true)
	s.Set(webauthnInviteIDKey, invite.ID.String())

	state := &setupPageState{
		Action:        action,
		InviteID:      invite.ID.String(),
		IsInviteSetup: true,
		Role:          invite.Role,
		RoleLabel:     invite.Role.Label(),
	}
	if invite.DisplayName != nil {
		state.DisplayName = *invite.DisplayName
	}

	return state, http.StatusOK, ""
}

func humanizeSetupError(err error) string {
	switch err {
	case nil:
		return ""
	case db.ErrSetupAlreadyCompleted:
		return "Setup already completed"
	case db.ErrInviteInvalidOrUsed:
		return "Invite is no longer valid"
	case db.ErrDisplayNameRequired:
		return "Display name is required"
	case db.ErrUsernameRequired:
		return "Username is required"
	case db.ErrInvalidUsername:
		return db.ErrInvalidUsername.Error()
	case db.ErrUsernameTaken:
		return "Username is already in use"
	case db.ErrPasswordRequired:
		return "Password is required"
	case db.ErrPasswordTooShort:
		return db.ErrPasswordTooShort.Error()
	case db.ErrInvalidRole:
		return "Invalid role"
	default:
		return "Failed to finalize setup"
	}
}
