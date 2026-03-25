/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/flamego/csrf"
	"github.com/flamego/flamego"
	"github.com/flamego/session"
	flamegoTemplate "github.com/flamego/template"
	"github.com/urfave/cli/v3"

	"github.com/humaidq/taalam/db"
	"github.com/humaidq/taalam/routes"
	"github.com/humaidq/taalam/static"
	"github.com/humaidq/taalam/templates"
)

// CmdStart defines the command that starts the web server.
var CmdStart = &cli.Command{
	Name:    "start",
	Aliases: []string{"run"},
	Usage:   "Start the web server",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "port",
			Value: "8080",
			Usage: "the web server port",
		},
		&cli.StringFlag{
			Name:    "database-url",
			Sources: cli.EnvVars("DATABASE_URL"),
			Usage:   "PostgreSQL connection string (e.g., postgres://user:pass@localhost/dbname)",
		},
	},
	Action: start,
}

func start(ctx context.Context, cmd *cli.Command) error {
	databaseURL := cmd.String("database-url")
	if databaseURL == "" {
		return errDatabaseURLRequired
	}

	csrfSecret := os.Getenv("CSRF_SECRET")
	if csrfSecret == "" {
		return errCSRFSecretRequired
	}

	if err := os.Setenv("DATABASE_URL", databaseURL); err != nil {
		return fmt.Errorf("failed to set DATABASE_URL: %w", err)
	}

	webAuthn, err := routes.NewWebAuthnFromEnv()
	if err != nil {
		return fmt.Errorf("failed to configure WebAuthn: %w", err)
	}

	appLogger.Info("connecting to database")

	if err := db.Init(ctx); err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}

	defer db.Close()

	appLogger.Info("syncing database schema")

	if err := db.SyncSchema(ctx); err != nil {
		return fmt.Errorf("failed to sync schema: %w", err)
	}

	f := flamego.New()
	configureEmptyNotFoundHandler(f)
	f.Use(flamego.Recovery())
	f.Map(webAuthn)
	f.Use(session.Sessioner(session.Options{
		Initer: db.PostgresSessionIniter(),
		Config: db.PostgresSessionConfig{
			Lifetime:  14 * 24 * time.Hour,
			TableName: "flamego_sessions",
		},
		Cookie: session.CookieOptions{
			MaxAge:   14 * 24 * 60 * 60,
			HTTPOnly: true,
			SameSite: http.SameSiteLaxMode,
		},
	}))
	f.Use(routes.RequestLogger)
	f.Use(csrf.Csrfer(csrf.Options{Secret: csrfSecret}))
	f.Use(routes.NoCacheHeaders())

	fs, err := flamegoTemplate.EmbedFS(templates.Templates, ".", []string{".html"})
	if err != nil {
		return fmt.Errorf("failed to load templates: %w", err)
	}

	f.Use(flamegoTemplate.Templater(flamegoTemplate.Options{
		FileSystem: fs,
	}))
	appVersion := BuildDisplayVersion()
	f.Use(func(data flamegoTemplate.Data, flash session.Flash) {
		data["AppVersion"] = appVersion

		if msg, ok := flash.(routes.FlashMessage); ok {
			data["Flash"] = msg
		}
	})
	f.Use(routes.CSRFInjector())
	f.Use(routes.UserContextInjector())

	f.Use(flamego.Static(flamego.StaticOptions{
		FileSystem: http.FS(static.Static),
	}))

	f.Get("/", routes.Dashboard)
	f.Get("/connectivity", routes.Connectivity)
	f.Get("/healthz", routes.Healthz)
	f.Get("/login", routes.LoginForm)
	f.Post("/login", csrf.Validate, routes.Login)
	f.Get("/setup", routes.SetupForm)
	f.Post("/setup", csrf.Validate, routes.SetupSubmit)
	f.Post("/webauthn/login/start", csrf.Validate, routes.PasskeyLoginStart)
	f.Post("/webauthn/login/finish", csrf.Validate, routes.PasskeyLoginFinish)

	f.Group("", func() {
		f.Post("/logout", csrf.Validate, routes.Logout)
		f.Get("/security", routes.Security)
		f.Post("/security/account/username", csrf.Validate, routes.UpdateAccountUsername)
		f.Post("/security/account/password", csrf.Validate, routes.UpdateAccountPassword)
		f.Post("/webauthn/passkey/start", csrf.Validate, routes.PasskeyRegistrationStart)
		f.Post("/webauthn/passkey/finish", csrf.Validate, routes.PasskeyRegistrationFinish)
		f.Post("/security/passkeys/{id}/delete", csrf.Validate, routes.DeletePasskey)
		f.Post("/security/invites", csrf.Validate, routes.CreateUserInvite)
		f.Post("/security/users/password", csrf.Validate, routes.CreatePasswordUser)
		f.Post("/security/invites/{id}/regenerate", csrf.Validate, routes.RegenerateUserInvite)
		f.Post("/security/invites/{id}/delete", csrf.Validate, routes.DeleteUserInvite)
	}, routes.RequireAuth)

	port := cmd.String("port")

	appLogger.Info("starting web server", "port", port)

	srv := &http.Server{
		Addr:              "0.0.0.0:" + port,
		Handler:           f,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("web server failed: %w", err)
	}

	return nil
}

func configureEmptyNotFoundHandler(f *flamego.Flame) {
	f.NotFound(func(c flamego.Context) {
		c.ResponseWriter().WriteHeader(http.StatusNotFound)
	})
}
