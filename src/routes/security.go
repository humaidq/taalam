/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package routes

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/flamego/flamego"
	"github.com/flamego/session"
	"github.com/flamego/template"

	"github.com/humaidq/taalam/db"
)

// PasskeyInfo represents a passkey entry on the account page.
type PasskeyInfo struct {
	ID        string
	Label     string
	CreatedAt time.Time
	LastUsed  *time.Time
	CanDelete bool
}

// InviteInfo represents a provisioning invite.
type InviteInfo struct {
	ID          string
	DisplayName string
	Role        string
	RoleLabel   string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	ExpiresIn   string
	IsExpired   bool
	SetupURL    string
}

type generatedAccountInfo struct {
	DisplayName string
	Username    string
	Password    string
	RoleLabel   string
}

type managedUserInfo struct {
	ID            string
	DisplayName   string
	Username      string
	Role          string
	RoleLabel     string
	HasPassword   bool
	CreatedAt     string
	UpdatedAt     string
	DeactivatedAt string
	IsDeactivated bool
	CanDeactivate bool
}

// Security renders the account management page.
func Security(c flamego.Context, s session.Session, t template.Template, data template.Data) {
	setPage(data, "Account")
	setBreadcrumbs(data, []BreadcrumbItem{
		homeBreadcrumb(),
		{Name: "Account", IsCurrent: true},
	})
	data["IsSecurity"] = true

	ctx := c.Request().Context()
	user, err := resolveSessionUser(ctx, s)
	if err != nil {
		data["Error"] = "Unable to resolve current user"
		t.HTML(http.StatusInternalServerError, "security")

		return
	}

	userID := user.ID.String()
	data["Username"] = valueOrEmpty(user.Username)
	data["HasPassword"] = user.HasPassword
	data["UserRole"] = string(user.Role)
	data["UserRoleLabel"] = user.Role.Label()

	passkeys, err := db.ListUserPasskeys(ctx, userID)
	if err != nil {
		data["Error"] = "Failed to load passkey information"
		t.HTML(http.StatusInternalServerError, "security")

		return
	}

	canDeletePasskeys := len(passkeys) > 1 || user.HasPassword
	passkeyInfos := make([]PasskeyInfo, 0, len(passkeys))
	for i, passkey := range passkeys {
		label := fmt.Sprintf("Passkey %d", i+1)
		if passkey.Label != nil && strings.TrimSpace(*passkey.Label) != "" {
			label = strings.TrimSpace(*passkey.Label)
		}

		passkeyInfos = append(passkeyInfos, PasskeyInfo{
			ID:        passkey.ID.String(),
			Label:     label,
			CreatedAt: passkey.CreatedAt,
			LastUsed:  passkey.LastUsedAt,
			CanDelete: canDeletePasskeys,
		})
	}

	data["Passkeys"] = passkeyInfos
	data["PasskeyCount"] = len(passkeyInfos)
	data["CanDeletePasskeys"] = canDeletePasskeys

	isAdmin := user.Role.IsAdmin()
	data["IsAdmin"] = isAdmin

	if generatedAccount := popGeneratedAccount(s); generatedAccount != nil {
		data["GeneratedAccount"] = generatedAccount
	}

	if isAdmin {
		baseSetupURL := buildExternalURL(c.Request(), "/setup")
		now := time.Now()

		invites, err := db.ListPendingUserInvites(ctx)
		if err != nil {
			logger.Error("failed to load invites", "error", err)
			data["InviteError"] = "Failed to load user invites"
		} else {
			inviteInfos := make([]InviteInfo, 0, len(invites))
			for _, invite := range invites {
				displayName := "New user"
				if invite.DisplayName != nil && strings.TrimSpace(*invite.DisplayName) != "" {
					displayName = strings.TrimSpace(*invite.DisplayName)
				}

				expiresAt := invite.CreatedAt.Add(24 * time.Hour)
				setupURL := baseSetupURL + "?token=" + url.QueryEscape(invite.Token)

				inviteInfos = append(inviteInfos, InviteInfo{
					ID:          invite.ID.String(),
					DisplayName: displayName,
					Role:        string(invite.Role),
					RoleLabel:   invite.Role.Label(),
					CreatedAt:   invite.CreatedAt,
					ExpiresAt:   expiresAt,
					ExpiresIn:   formatDuration(expiresAt.Sub(now)),
					IsExpired:   !expiresAt.After(now),
					SetupURL:    setupURL,
				})
			}

			data["UserInvites"] = inviteInfos
		}

		expiredInvites, err := db.ListExpiredUserInvites(ctx)
		if err != nil {
			logger.Error("failed to load expired invites", "error", err)
			data["InviteError"] = "Failed to load user invites"
		} else {
			expiredInviteInfos := make([]InviteInfo, 0, len(expiredInvites))
			for _, invite := range expiredInvites {
				displayName := "New user"
				if invite.DisplayName != nil && strings.TrimSpace(*invite.DisplayName) != "" {
					displayName = strings.TrimSpace(*invite.DisplayName)
				}

				expiresAt := invite.CreatedAt.Add(24 * time.Hour)
				expiredInviteInfos = append(expiredInviteInfos, InviteInfo{
					ID:          invite.ID.String(),
					DisplayName: displayName,
					Role:        string(invite.Role),
					RoleLabel:   invite.Role.Label(),
					CreatedAt:   invite.CreatedAt,
					ExpiresAt:   expiresAt,
					ExpiresIn:   formatDuration(expiresAt.Sub(now)),
					IsExpired:   true,
				})
			}

			data["ExpiredUserInvites"] = expiredInviteInfos
		}

		managedUsers, err := db.ListManagedUsers(ctx)
		if err != nil {
			logger.Error("failed to load users", "error", err)
			data["InviteError"] = "Failed to load users"
		} else {
			userItems := make([]managedUserInfo, 0, len(managedUsers))
			for _, managedUser := range managedUsers {
				item := managedUserInfo{
					ID:            managedUser.ID.String(),
					DisplayName:   managedUser.DisplayName,
					Username:      valueOrEmpty(managedUser.Username),
					Role:          string(managedUser.Role),
					RoleLabel:     managedUser.Role.Label(),
					HasPassword:   managedUser.HasPassword,
					CreatedAt:     managedUser.CreatedAt.Format("Jan 2, 2006"),
					UpdatedAt:     managedUser.UpdatedAt.Format("Jan 2, 2006 15:04"),
					CanDeactivate: managedUser.ID != user.ID,
				}
				if managedUser.DeactivatedAt != nil {
					item.IsDeactivated = true
					item.DeactivatedAt = managedUser.DeactivatedAt.Format("Jan 2, 2006 15:04")
				}

				userItems = append(userItems, item)
			}

			data["ManagedUsers"] = userItems
		}
	}

	t.HTML(http.StatusOK, "security")
}

// DeletePasskey removes a passkey for the current user.
func DeletePasskey(c flamego.Context, s session.Session) {
	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	user, err := db.GetUserByID(c.Request().Context(), userID)
	if err != nil {
		SetErrorFlash(s, "Failed to load account settings")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	count, err := db.CountUserPasskeys(c.Request().Context(), userID)
	if err != nil {
		SetErrorFlash(s, "Failed to load passkeys")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	if count <= 1 && !user.HasPassword {
		SetWarningFlash(s, "Set a password before deleting your last passkey")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	passkeyID := c.Param("id")
	if passkeyID == "" {
		SetErrorFlash(s, "Missing passkey ID")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	if err := db.DeleteUserPasskey(c.Request().Context(), userID, passkeyID); err != nil {
		SetErrorFlash(s, "Failed to delete passkey")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	SetSuccessFlash(s, "Passkey deleted")
	c.Redirect("/security", http.StatusSeeOther)
}

// CreateUserInvite generates a new invite token (admin only).
func CreateUserInvite(c flamego.Context, s session.Session) {
	ctx := c.Request().Context()

	isAdmin, err := resolveSessionIsAdmin(ctx, s)
	if err != nil || !isAdmin {
		SetErrorFlash(s, "Access restricted")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	if err := c.Request().ParseForm(); err != nil {
		SetErrorFlash(s, "Failed to parse form")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	userID, _ := getSessionUserID(s)
	displayName := strings.TrimSpace(c.Request().Form.Get("display_name"))
	role, err := db.NormalizeRole(c.Request().Form.Get("role"))
	if err != nil {
		SetErrorFlash(s, "Choose a valid role")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	if _, err := db.CreateUserInvite(ctx, userID, displayName, role); err != nil {
		SetErrorFlash(s, "Failed to create invite")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	SetSuccessFlash(s, "Invite created")
	c.Redirect("/security", http.StatusSeeOther)
}

// CreatePasswordUser creates a password-based account and reveals the generated credentials once.
func CreatePasswordUser(c flamego.Context, s session.Session) {
	ctx := c.Request().Context()

	isAdmin, err := resolveSessionIsAdmin(ctx, s)
	if err != nil || !isAdmin {
		SetErrorFlash(s, "Access restricted")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	if err := c.Request().ParseForm(); err != nil {
		SetErrorFlash(s, "Failed to parse form")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	role, err := db.NormalizeRole(c.Request().Form.Get("role"))
	if err != nil {
		SetErrorFlash(s, "Choose a valid role")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	username := strings.TrimSpace(c.Request().Form.Get("username"))
	displayName := strings.TrimSpace(c.Request().Form.Get("display_name"))
	password, err := generatePassword(12)
	if err != nil {
		SetErrorFlash(s, "Failed to generate password")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	user, err := db.CreatePasswordUser(ctx, db.CreatePasswordUserInput{
		DisplayName: displayName,
		Username:    username,
		Password:    password,
		Role:        role,
	})
	if err != nil {
		SetErrorFlash(s, humanizeAccountCreationError(err))
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	storeGeneratedAccount(s, generatedAccountInfo{
		DisplayName: user.DisplayName,
		Username:    valueOrEmpty(user.Username),
		Password:    password,
		RoleLabel:   user.Role.Label(),
	})
	SetSuccessFlash(s, "Account created")
	c.Redirect("/security", http.StatusSeeOther)
}

// UpdateAccountUsername updates the current user's username.
func UpdateAccountUsername(c flamego.Context, s session.Session) {
	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	if err := c.Request().ParseForm(); err != nil {
		SetErrorFlash(s, "Failed to parse form")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	user, err := db.UpdateUsername(c.Request().Context(), userID, c.Request().Form.Get("username"))
	if err != nil {
		SetErrorFlash(s, humanizeAccountCreationError(err))
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	s.Set("user_display_name", user.DisplayName)
	s.Set("user_role", string(user.Role))
	s.Set("user_is_admin", user.Role.IsAdmin())
	SetSuccessFlash(s, "Username updated")
	c.Redirect("/security", http.StatusSeeOther)
}

// UpdateAccountPassword updates the current user's password.
func UpdateAccountPassword(c flamego.Context, s session.Session) {
	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	if err := c.Request().ParseForm(); err != nil {
		SetErrorFlash(s, "Failed to parse form")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	password := c.Request().Form.Get("password")
	confirmPassword := c.Request().Form.Get("confirm_password")
	if password != confirmPassword {
		SetErrorFlash(s, "Passwords do not match")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	if err := db.SetUserPassword(c.Request().Context(), userID, password); err != nil {
		SetErrorFlash(s, humanizeAccountCreationError(err))
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	SetSuccessFlash(s, "Password updated")
	c.Redirect("/security", http.StatusSeeOther)
}

// RegenerateUserInvite refreshes an expired invite link (admin only).
func RegenerateUserInvite(c flamego.Context, s session.Session) {
	ctx := c.Request().Context()

	isAdmin, err := resolveSessionIsAdmin(ctx, s)
	if err != nil || !isAdmin {
		SetErrorFlash(s, "Access restricted")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	inviteID := strings.TrimSpace(c.Param("id"))
	if inviteID == "" {
		SetErrorFlash(s, "Missing invite ID")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	if _, err := db.RegenerateExpiredUserInvite(ctx, inviteID); err != nil {
		switch {
		case errors.Is(err, db.ErrInviteNotExpired):
			SetWarningFlash(s, "Invite has not expired yet")
		default:
			SetErrorFlash(s, "Failed to regenerate invite")
		}

		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	SetSuccessFlash(s, "Invite link regenerated")
	c.Redirect("/security", http.StatusSeeOther)
}

// DeleteUserInvite revokes a pending invite (admin only).
func DeleteUserInvite(c flamego.Context, s session.Session) {
	ctx := c.Request().Context()

	isAdmin, err := resolveSessionIsAdmin(ctx, s)
	if err != nil || !isAdmin {
		SetErrorFlash(s, "Access restricted")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	inviteID := c.Param("id")
	if inviteID == "" {
		SetErrorFlash(s, "Missing invite ID")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	if err := db.DeleteUserInvite(ctx, inviteID); err != nil {
		SetErrorFlash(s, "Failed to revoke invite")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	SetSuccessFlash(s, "Invite revoked")
	c.Redirect("/security", http.StatusSeeOther)
}

// UpdateManagedUser updates an admin-managed user account.
func UpdateManagedUser(c flamego.Context, s session.Session) {
	ctx := c.Request().Context()

	isAdmin, err := resolveSessionIsAdmin(ctx, s)
	if err != nil || !isAdmin {
		SetErrorFlash(s, "Access restricted")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	if err := c.Request().ParseForm(); err != nil {
		SetErrorFlash(s, "Failed to parse form")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	actorUserID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	role, err := db.NormalizeRole(c.Request().Form.Get("role"))
	if err != nil {
		SetErrorFlash(s, "Choose a valid role")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	if _, err := db.UpdateManagedUser(ctx, db.UpdateManagedUserInput{
		UserID:      c.Param("id"),
		DisplayName: c.Request().Form.Get("display_name"),
		Username:    c.Request().Form.Get("username"),
		Role:        role,
		UpdatedBy:   actorUserID,
	}); err != nil {
		SetErrorFlash(s, humanizeAccountCreationError(err))
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	SetSuccessFlash(s, "User updated")
	c.Redirect("/security", http.StatusSeeOther)
}

// DeactivateUser disables a user account.
func DeactivateUser(c flamego.Context, s session.Session) {
	ctx := c.Request().Context()

	isAdmin, err := resolveSessionIsAdmin(ctx, s)
	if err != nil || !isAdmin {
		SetErrorFlash(s, "Access restricted")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	actorUserID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	if err := db.DeactivateUser(ctx, actorUserID, c.Param("id")); err != nil {
		SetErrorFlash(s, humanizeAccountCreationError(err))
		c.Redirect("/security", http.StatusSeeOther)

		return
	}

	SetSuccessFlash(s, "User deactivated")
	c.Redirect("/security", http.StatusSeeOther)
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		return "expired"
	}

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	parts := make([]string, 0, 2)
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}

	if days > 0 {
		if hours > 0 {
			parts = append(parts, fmt.Sprintf("%dh", hours))
		} else if minutes > 0 {
			parts = append(parts, fmt.Sprintf("%dm", minutes))
		}
	} else if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
		if minutes > 0 {
			parts = append(parts, fmt.Sprintf("%dm", minutes))
		}
	} else {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}

	if len(parts) == 0 {
		return "in 0m"
	}

	if len(parts) == 1 {
		return "in " + parts[0]
	}

	return "in " + parts[0] + " " + parts[1]
}

func buildExternalURL(r *flamego.Request, path string) string {
	scheme := "http"
	if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		scheme = strings.TrimSpace(strings.Split(proto, ",")[0])
	} else if r.TLS != nil {
		scheme = "https"
	}

	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}

	if host == "" {
		return path
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return scheme + "://" + host + path
}

func generatePassword(length int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	if length <= 0 {
		return "", nil
	}

	buffer := make([]byte, length)
	randomBytes := make([]byte, length)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	for i := range buffer {
		buffer[i] = alphabet[int(randomBytes[i])%len(alphabet)]
	}

	return string(buffer), nil
}

func humanizeAccountCreationError(err error) string {
	switch err {
	case nil:
		return ""
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
		return "Choose a valid role"
	case db.ErrUserNotFound:
		return "User not found"
	case db.ErrUserAlreadyDeactivated:
		return "User is already deactivated"
	case db.ErrUserDeactivated:
		return "Deactivated users cannot be modified"
	case db.ErrCannotDeactivateCurrentUser:
		return "You cannot deactivate your own account"
	case db.ErrActiveAdminRequired:
		return "At least one active admin account must remain"
	default:
		return "Failed to save account"
	}
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}

	return strings.TrimSpace(*value)
}

func storeGeneratedAccount(s session.Session, info generatedAccountInfo) {
	s.Set("generated_account_display_name", info.DisplayName)
	s.Set("generated_account_username", info.Username)
	s.Set("generated_account_password", info.Password)
	s.Set("generated_account_role_label", info.RoleLabel)
}

func popGeneratedAccount(s session.Session) *generatedAccountInfo {
	username, ok := s.Get("generated_account_username").(string)
	if !ok || strings.TrimSpace(username) == "" {
		return nil
	}

	info := &generatedAccountInfo{
		DisplayName: valueOrEmptyString(s.Get("generated_account_display_name")),
		Username:    username,
		Password:    valueOrEmptyString(s.Get("generated_account_password")),
		RoleLabel:   valueOrEmptyString(s.Get("generated_account_role_label")),
	}

	s.Delete("generated_account_display_name")
	s.Delete("generated_account_username")
	s.Delete("generated_account_password")
	s.Delete("generated_account_role_label")

	return info
}

func valueOrEmptyString(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(text)
}
