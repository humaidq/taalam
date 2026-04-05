/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package routes

import (
	"net/http"

	"github.com/flamego/flamego"
	"github.com/flamego/session"
	"github.com/flamego/template"
)

type BreadcrumbItem struct {
	Name      string
	URL       string
	IsCurrent bool
}

func homeBreadcrumb() BreadcrumbItem {
	return BreadcrumbItem{Name: "Home", URL: "/"}
}

func coursesBreadcrumbs() []BreadcrumbItem {
	return []BreadcrumbItem{
		homeBreadcrumb(),
		{Name: "Courses", URL: "/courses", IsCurrent: true},
	}
}

func courseBreadcrumbs(course courseListItem) []BreadcrumbItem {
	return []BreadcrumbItem{
		homeBreadcrumb(),
		{Name: "Courses", URL: "/courses"},
		{Name: courseBreadcrumbLabel(course), URL: "/courses/" + course.ID, IsCurrent: true},
	}
}

func courseBreadcrumbLabel(course courseListItem) string {
	if course.Code != "" {
		return course.Code
	}

	return course.Title
}

func assignmentBreadcrumbs(course courseListItem, assignment assignmentListItem, current string) []BreadcrumbItem {
	items := []BreadcrumbItem{
		homeBreadcrumb(),
		{Name: "Courses", URL: "/courses"},
		{Name: courseBreadcrumbLabel(course), URL: "/courses/" + course.ID},
		{Name: assignment.Title, URL: "/courses/" + course.ID + "/assignments/" + assignment.ID},
	}
	items[len(items)-1].IsCurrent = current == "assignment"
	if current == "assignment" {
		return items
	}

	return append(items, BreadcrumbItem{Name: current, IsCurrent: true})
}

func setPage(data template.Data, title string) {
	data["PageTitle"] = title
}

func setBreadcrumbs(data template.Data, items []BreadcrumbItem) {
	data["Breadcrumbs"] = items
}

func redirectWithMessage(c flamego.Context, s session.Session, path string, messageType FlashType, message string) {
	SetFlash(s, messageType, message)
	c.Redirect(path, http.StatusSeeOther)
}
