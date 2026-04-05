/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package routes

import (
	"net/http"
	"os"
	"strings"

	"github.com/flamego/flamego"
	"github.com/flamego/template"

	"github.com/humaidq/taalam/db"
)

func writePlain(c flamego.Context, value string) {
	if _, err := c.ResponseWriter().Write([]byte(value)); err != nil {
		logger.Error("failed to write response", "error", err)
	}
}

// Connectivity returns a tiny endpoint for online checks.
func Connectivity(c flamego.Context) {
	writePlain(c, "1")
}

// Healthz returns a simple health endpoint.
func Healthz(c flamego.Context) {
	writePlain(c, "ok")
}

// Dashboard renders the main landing page.
func Dashboard(c flamego.Context, t template.Template, data template.Data) {
	setPage(data, "Home")
	setBreadcrumbs(data, []BreadcrumbItem{{Name: "Home", IsCurrent: true}})
	data["IsDashboard"] = true

	userCount, err := db.CountUsers(c.Request().Context())
	if err != nil {
		logger.Error("failed to load user count", "error", err)
		setPageErrorFlash(data, "Failed to load account status")
	}

	data["UserCount"] = userCount
	data["BootstrapRequired"] = userCount == 0
	data["BootstrapConfigured"] = strings.TrimSpace(os.Getenv("BOOTSTRAP_TOKEN")) != ""

	t.HTML(http.StatusOK, "dashboard")
}
