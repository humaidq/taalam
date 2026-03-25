/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package db

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

const minPasswordLength = 8

var usernamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{1,30}[a-z0-9])?$`)

// UserRole identifies a user's system role.
type UserRole string

// Supported user roles.
const (
	RoleAdmin   UserRole = "admin"
	RoleTeacher UserRole = "teacher"
	RoleStudent UserRole = "student"
)

// IsAdmin reports whether the role grants administrative access.
func (r UserRole) IsAdmin() bool {
	return r == RoleAdmin
}

// Valid reports whether the role is supported.
func (r UserRole) Valid() bool {
	switch r {
	case RoleAdmin, RoleTeacher, RoleStudent:
		return true
	default:
		return false
	}
}

// Label returns a human-readable role name.
func (r UserRole) Label() string {
	switch r {
	case RoleAdmin:
		return "Admin"
	case RoleTeacher:
		return "Teacher"
	case RoleStudent:
		return "Student"
	default:
		return string(r)
	}
}

// NormalizeRole validates and normalizes a role string.
func NormalizeRole(raw string) (UserRole, error) {
	role := UserRole(strings.ToLower(strings.TrimSpace(raw)))
	if !role.Valid() {
		return "", ErrInvalidRole
	}

	return role, nil
}

// NormalizeUsername validates and normalizes a login username.
func NormalizeUsername(raw string) (string, error) {
	username := strings.ToLower(strings.TrimSpace(raw))
	if username == "" {
		return "", ErrUsernameRequired
	}

	if !usernamePattern.MatchString(username) {
		return "", ErrInvalidUsername
	}

	return username, nil
}

// ValidatePassword checks that a password meets minimum requirements.
func ValidatePassword(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return ErrPasswordRequired
	}

	if len(raw) < minPasswordLength {
		return ErrPasswordTooShort
	}

	return nil
}

// HashPassword hashes a password with bcrypt.
func HashPassword(raw string) (string, error) {
	if err := ValidatePassword(raw); err != nil {
		return "", err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}

	return string(hash), nil
}

// CreatePasswordUserInput defines a direct username/password account creation.
type CreatePasswordUserInput struct {
	DisplayName string
	Username    string
	Password    string
	Role        UserRole
}

// FinalizeSetupPasswordInput defines username/password setup completion.
type FinalizeSetupPasswordInput struct {
	UserID      uuid.UUID
	DisplayName string
	Username    string
	Password    string
	Role        UserRole
	InviteID    *string
}

// AuthenticateUserWithPassword verifies username/password credentials.
func AuthenticateUserWithPassword(ctx context.Context, username string, password string) (*User, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	normalizedUsername, err := NormalizeUsername(username)
	if err != nil {
		return nil, ErrInvalidUsernameOrPassword
	}

	var (
		user         User
		storedRole   string
		passwordHash *string
	)

	err = pool.QueryRow(ctx, `
		SELECT id, display_name, username, role, password_hash IS NOT NULL, password_hash, created_at, updated_at
		FROM users
		WHERE username = $1
	`, normalizedUsername).Scan(
		&user.ID,
		&user.DisplayName,
		&user.Username,
		&storedRole,
		&user.HasPassword,
		&passwordHash,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidUsernameOrPassword
	}

	if err != nil {
		return nil, fmt.Errorf("failed to load user by username: %w", err)
	}

	user.Role = UserRole(storedRole)
	if !user.Role.Valid() || passwordHash == nil {
		return nil, ErrInvalidUsernameOrPassword
	}

	if err := bcrypt.CompareHashAndPassword([]byte(*passwordHash), []byte(password)); err != nil {
		return nil, ErrInvalidUsernameOrPassword
	}

	return &user, nil
}

// CreatePasswordUser creates an account with a username and password.
func CreatePasswordUser(ctx context.Context, input CreatePasswordUserInput) (*User, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	role, err := NormalizeRole(string(input.Role))
	if err != nil {
		return nil, err
	}

	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(input.Username)
	}
	if displayName == "" {
		return nil, ErrDisplayNameRequired
	}

	username, err := NormalizeUsername(input.Username)
	if err != nil {
		return nil, err
	}

	passwordHash, err := HashPassword(input.Password)
	if err != nil {
		return nil, err
	}

	userID := uuid.New()

	user, err := insertUser(ctx, userID, displayName, &username, &passwordHash, role)
	if err != nil {
		return nil, err
	}

	return user, nil
}

// FinalizeSetupPasswordAccount creates the first admin or an invited account with a password.
func FinalizeSetupPasswordAccount(ctx context.Context, input FinalizeSetupPasswordInput) (*User, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	role, err := NormalizeRole(string(input.Role))
	if err != nil {
		return nil, err
	}

	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		return nil, ErrDisplayNameRequired
	}

	username, err := NormalizeUsername(input.Username)
	if err != nil {
		return nil, err
	}

	passwordHash, err := HashPassword(input.Password)
	if err != nil {
		return nil, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start setup transaction: %w", err)
	}

	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			logger.Warn("failed to rollback setup transaction", "error", rollbackErr)
		}
	}()

	if input.InviteID == nil {
		if !role.IsAdmin() {
			return nil, ErrAdminRequired
		}

		if _, err := tx.Exec(ctx, `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			return nil, fmt.Errorf("failed to lock users table: %w", err)
		}

		var count int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
			return nil, fmt.Errorf("failed to count users: %w", err)
		}

		if count > 0 {
			return nil, ErrSetupAlreadyCompleted
		}
	} else {
		if err := consumeInviteTx(ctx, tx, *input.InviteID); err != nil {
			return nil, err
		}
	}

	user, err := insertUserTx(ctx, tx, input.UserID, displayName, &username, &passwordHash, role)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit setup transaction: %w", err)
	}

	return user, nil
}

// UpdateUsername updates a user's username.
func UpdateUsername(ctx context.Context, userID string, username string) (*User, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	normalizedUsername, err := NormalizeUsername(username)
	if err != nil {
		return nil, err
	}

	var (
		user       User
		storedRole string
	)

	err = pool.QueryRow(ctx, `
		UPDATE users
		SET username = $1
		WHERE id = $2
		RETURNING id, display_name, username, role, password_hash IS NOT NULL, created_at, updated_at
	`, normalizedUsername, userID).Scan(
		&user.ID,
		&user.DisplayName,
		&user.Username,
		&storedRole,
		&user.HasPassword,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}

	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrUsernameTaken
		}

		return nil, fmt.Errorf("failed to update username: %w", err)
	}

	user.Role = UserRole(storedRole)

	return &user, nil
}

// SetUserPassword stores a bcrypt password for a user.
func SetUserPassword(ctx context.Context, userID string, password string) error {
	if pool == nil {
		return ErrDatabaseConnectionNotInitialized
	}

	passwordHash, err := HashPassword(password)
	if err != nil {
		return err
	}

	command, err := pool.Exec(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2`, passwordHash, userID)
	if err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	if command.RowsAffected() == 0 {
		return ErrUserNotFound
	}

	return nil
}

func insertUser(ctx context.Context, userID uuid.UUID, displayName string, username *string, passwordHash *string, role UserRole) (*User, error) {
	if pool == nil {
		return nil, ErrDatabaseConnectionNotInitialized
	}

	var (
		user       User
		storedRole string
	)

	err := pool.QueryRow(ctx, `
		INSERT INTO users (id, display_name, username, password_hash, role, is_admin)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, display_name, username, role, password_hash IS NOT NULL, created_at, updated_at
	`, userID, displayName, username, passwordHash, role, role.IsAdmin()).Scan(
		&user.ID,
		&user.DisplayName,
		&user.Username,
		&storedRole,
		&user.HasPassword,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrUsernameTaken
		}

		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	user.Role = UserRole(storedRole)

	return &user, nil
}

func insertUserTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, displayName string, username *string, passwordHash *string, role UserRole) (*User, error) {
	var (
		user       User
		storedRole string
	)

	err := tx.QueryRow(ctx, `
		INSERT INTO users (id, display_name, username, password_hash, role, is_admin)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, display_name, username, role, password_hash IS NOT NULL, created_at, updated_at
	`, userID, displayName, username, passwordHash, role, role.IsAdmin()).Scan(
		&user.ID,
		&user.DisplayName,
		&user.Username,
		&storedRole,
		&user.HasPassword,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrUsernameTaken
		}

		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	user.Role = UserRole(storedRole)

	return &user, nil
}

func consumeInviteTx(ctx context.Context, tx pgx.Tx, inviteID string) error {
	trimmedInviteID := strings.TrimSpace(inviteID)
	if trimmedInviteID == "" {
		return ErrInviteInvalidOrUsed
	}

	if _, err := uuid.Parse(trimmedInviteID); err != nil {
		return ErrInviteInvalidOrUsed
	}

	command, err := tx.Exec(ctx, `
		UPDATE user_invites
		SET used_at = NOW()
		WHERE id = $1
		  AND used_at IS NULL
		  AND created_at >= NOW() - INTERVAL '24 hours'
	`, trimmedInviteID)
	if err != nil {
		return fmt.Errorf("failed to consume invite: %w", err)
	}

	if command.RowsAffected() == 0 {
		return ErrInviteInvalidOrUsed
	}

	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.Code == "23505"
}
