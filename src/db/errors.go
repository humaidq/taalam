/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package db

import "errors"

var (
	ErrDatabaseConnectionNotInitialized = errors.New("database connection not initialized")
	ErrDatabaseURLEnvVarNotSet          = errors.New("DATABASE_URL environment variable is not set")
	ErrDatabaseNameNotSpecified         = errors.New("no database name specified in DATABASE_URL")
	ErrDisplayNameRequired              = errors.New("display name is required")
	ErrUsernameRequired                 = errors.New("username is required")
	ErrInvalidUsername                  = errors.New("username must be 3-32 characters and use only lowercase letters, numbers, dots, dashes, or underscores")
	ErrUsernameTaken                    = errors.New("username is already in use")
	ErrPasswordRequired                 = errors.New("password is required")
	ErrPasswordTooShort                 = errors.New("password must be at least 8 characters")
	ErrInvalidUsernameOrPassword        = errors.New("invalid username or password")
	ErrInvalidRole                      = errors.New("invalid role")
	ErrUserNotFound                     = errors.New("user not found")
	ErrInvalidUserHandle                = errors.New("invalid user handle")
	ErrPasskeyNotFound                  = errors.New("passkey not found")
	ErrSetupAlreadyCompleted            = errors.New("setup already completed")
	ErrInviteInvalidOrUsed              = errors.New("invite is no longer valid")
	ErrInvalidCreatorID                 = errors.New("invalid creator ID")
	ErrInviteNotFound                   = errors.New("invite not found")
	ErrInviteNotExpired                 = errors.New("invite is not expired")
	ErrAccessDenied                     = errors.New("access denied")
	ErrAdminRequired                    = errors.New("admin required")

	ErrInvalidPostgresSessionIniterArgument = errors.New("invalid PostgresSessionIniter argument")
)
