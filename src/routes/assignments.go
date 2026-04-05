/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package routes

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/flamego/flamego"
	"github.com/flamego/session"
	"github.com/flamego/template"

	"github.com/humaidq/taalam/db"
)

type assignmentListItem struct {
	ID           string
	CourseID     string
	Title        string
	Description  string
	DueAt        string
	DueAtInput   string
	MaxGrade     string
	CreatedAt    string
	MetadataHash string
}

// NewAssignmentForm renders the assignment creation page for a course.
func NewAssignmentForm(c flamego.Context, s session.Session, t template.Template, data template.Data) {
	setPage(data, "New Assignment")
	data["IsCourses"] = true

	ctx := c.Request().Context()
	user, err := resolveSessionUser(ctx, s)
	if err != nil {
		SetErrorFlash(s, "Unable to resolve current user")
		redirectCoursePath(c, c.Param("id"))

		return
	}

	courseID := strings.TrimSpace(c.Param("id"))
	canManage, err := db.CanUserManageCourse(ctx, user.ID.String(), courseID)
	if err != nil || !canManage {
		SetErrorFlash(s, "Access restricted")
		redirectCoursePath(c, courseID)

		return
	}

	course, err := db.GetCourseByID(ctx, courseID)
	if err != nil {
		SetErrorFlash(s, "Course not found")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	outlineItems, err := loadCourseOutlineItems(ctx, courseID)
	if err != nil {
		SetErrorFlash(s, "Failed to load course content")
		redirectCoursePath(c, courseID)

		return
	}

	data["Course"] = makeCourseListItem(*course)
	setBreadcrumbs(data, []BreadcrumbItem{
		homeBreadcrumb(),
		{Name: "Courses", URL: "/courses"},
		{Name: courseBreadcrumbLabel(data["Course"].(courseListItem)), URL: "/courses/" + course.ID},
		{Name: "New Assignment", IsCurrent: true},
	})
	data["OutlineInsertOptions"] = buildOutlineInsertOptions(outlineItems)
	t.HTML(http.StatusOK, "assignment_new")
}

// CreateAssignment handles assignment creation for a course.
func CreateAssignment(c flamego.Context, s session.Session) {
	if err := c.Request().ParseForm(); err != nil {
		SetErrorFlash(s, "Failed to parse form")
		redirectCoursePath(c, c.Param("id"))

		return
	}

	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		redirectCoursePath(c, c.Param("id"))

		return
	}

	dueAt, err := parseOptionalDateTimeLocal(c.Request().Form.Get("due_at"))
	if err != nil {
		SetErrorFlash(s, "Choose a valid due date")
		redirectCoursePath(c, c.Param("id"))

		return
	}

	assignment, err := db.CreateAssignment(c.Request().Context(), db.CreateAssignmentInput{
		CourseID:          c.Param("id"),
		Title:             c.Request().Form.Get("title"),
		Description:       c.Request().Form.Get("description"),
		DueAt:             dueAt,
		MaxGrade:          c.Request().Form.Get("max_grade"),
		InsertAfterItemID: c.Request().Form.Get("insert_after_item_id"),
		CreatedBy:         userID,
	})
	if err != nil {
		SetErrorFlash(s, humanizeAssignmentWriteError(err))
		redirectCoursePath(c, c.Param("id"))

		return
	}

	SetSuccessFlash(s, "Assignment published")
	c.Redirect("/courses/"+assignment.CourseID+"/assignments/"+assignment.ID, http.StatusSeeOther)
}

// UpdateAssignment handles assignment updates.
func UpdateAssignment(c flamego.Context, s session.Session) {
	if err := c.Request().ParseForm(); err != nil {
		SetErrorFlash(s, "Failed to parse form")
		redirectCoursePath(c, c.Param("id"))

		return
	}

	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		redirectCoursePath(c, c.Param("id"))

		return
	}

	dueAt, err := parseOptionalDateTimeLocal(c.Request().Form.Get("due_at"))
	if err != nil {
		SetErrorFlash(s, "Choose a valid due date")
		redirectCoursePath(c, c.Param("id"))

		return
	}

	assignment, err := db.UpdateAssignment(c.Request().Context(), db.UpdateAssignmentInput{
		AssignmentID: c.Param("assignmentID"),
		CourseID:     c.Param("id"),
		Title:        c.Request().Form.Get("title"),
		Description:  c.Request().Form.Get("description"),
		DueAt:        dueAt,
		MaxGrade:     c.Request().Form.Get("max_grade"),
		UpdatedBy:    userID,
	})
	if err != nil {
		SetErrorFlash(s, humanizeAssignmentWriteError(err))
		redirectCoursePath(c, c.Param("id"))

		return
	}

	SetSuccessFlash(s, "Assignment updated")
	c.Redirect("/courses/"+assignment.CourseID+"/assignments/"+assignment.ID, http.StatusSeeOther)
}

// DeleteAssignment removes an assignment.
func DeleteAssignment(c flamego.Context, s session.Session) {
	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		redirectCoursePath(c, c.Param("id"))

		return
	}

	if err := db.DeleteAssignment(c.Request().Context(), c.Param("id"), c.Param("assignmentID"), userID); err != nil {
		SetErrorFlash(s, humanizeAssignmentWriteError(err))
		redirectCoursePath(c, c.Param("id"))

		return
	}

	SetSuccessFlash(s, "Assignment deleted")
	redirectCoursePath(c, c.Param("id"))
}

// AssignmentDetail renders one assignment.
func AssignmentDetail(c flamego.Context, s session.Session, t template.Template, data template.Data) {
	setPage(data, "Assignment")
	data["IsCourses"] = true

	ctx := c.Request().Context()
	user, err := resolveSessionUser(ctx, s)
	if err != nil {
		SetErrorFlash(s, "Unable to resolve current user")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	assignmentID := strings.TrimSpace(c.Param("assignmentID"))
	if assignmentID == "" {
		SetErrorFlash(s, "Missing assignment ID")
		redirectCoursePath(c, c.Param("id"))

		return
	}

	canView, err := db.CanUserViewAssignment(ctx, user.ID.String(), assignmentID)
	if err != nil {
		logger.Error("failed to check assignment visibility", "error", err)
		SetErrorFlash(s, "Failed to load assignment")
		redirectCoursePath(c, c.Param("id"))

		return
	}
	if !canView {
		SetErrorFlash(s, "Access restricted")
		redirectCoursePath(c, c.Param("id"))

		return
	}

	assignment, err := db.GetAssignmentByID(ctx, assignmentID)
	if err != nil {
		if errors.Is(err, db.ErrAssignmentNotFound) {
			SetErrorFlash(s, "Assignment not found")
			redirectCoursePath(c, c.Param("id"))

			return
		}

		logger.Error("failed to get assignment", "error", err)
		SetErrorFlash(s, "Failed to load assignment")
		redirectCoursePath(c, c.Param("id"))

		return
	}

	courseID := strings.TrimSpace(c.Param("id"))
	if assignment.CourseID != courseID {
		SetErrorFlash(s, "Assignment not found")
		redirectCoursePath(c, courseID)

		return
	}

	course, err := db.GetCourseByID(ctx, assignment.CourseID)
	if err != nil {
		logger.Error("failed to load assignment course", "error", err)
		SetErrorFlash(s, "Failed to load assignment")
		redirectCoursePath(c, courseID)

		return
	}

	canManage, err := db.CanUserManageAssignment(ctx, user.ID.String(), assignment.ID)
	if err != nil {
		logger.Error("failed to check assignment management access", "error", err)
		SetErrorFlash(s, "Failed to load assignment")
		redirectCoursePath(c, courseID)

		return
	}

	data["Course"] = makeCourseListItem(*course)
	data["Assignment"] = makeAssignmentListItem(*assignment)
	setBreadcrumbs(data, assignmentBreadcrumbs(data["Course"].(courseListItem), data["Assignment"].(assignmentListItem), "assignment"))
	data["CanManageAssignment"] = canManage

	if canManage {
		submissions, err := db.ListSubmissionsForAssignment(ctx, assignment.ID)
		if err != nil {
			logger.Error("failed to list assignment submissions", "error", err)
			SetErrorFlash(s, "Failed to load assignment")
			redirectCoursePath(c, courseID)

			return
		}

		data["Submissions"] = makeSubmissionListItems(submissions)
	}

	if user.Role.IsStudent() {
		canSubmit, err := db.CanUserSubmitAssignment(ctx, user.ID.String(), assignment.ID)
		if err != nil {
			logger.Error("failed to check assignment submission access", "error", err)
			SetErrorFlash(s, "Failed to load assignment")
			redirectCoursePath(c, courseID)

			return
		}

		data["CanSubmitAssignment"] = canSubmit
		data["AssignmentPastDue"] = assignment.DueAt != nil && time.Now().UTC().After(*assignment.DueAt)

		latestSubmission, err := db.GetLatestSubmissionForStudentAssignment(ctx, user.ID.String(), assignment.ID)
		if err != nil {
			logger.Error("failed to load latest submission", "error", err)
			SetErrorFlash(s, "Failed to load assignment")
			redirectCoursePath(c, courseID)

			return
		}

		if latestSubmission != nil {
			data["LatestSubmission"] = submissionReceiptItem{
				ID:           latestSubmission.ID,
				AssignmentID: latestSubmission.AssignmentID,
				Version:      latestSubmission.Version,
				FileName:     latestSubmission.FileName,
				ContentType:  latestSubmission.ContentType,
				FileSize:     latestSubmission.FileSize,
				FileSHA256:   latestSubmission.FileSHA256,
				SubmittedAt:  latestSubmission.SubmittedAt.Format("Jan 2, 2006 15:04:05 MST"),
			}
		}

		latestGrade, err := db.GetGradeVerificationForAssignmentStudent(ctx, assignment.ID, user.ID.String())
		if err != nil && !errors.Is(err, db.ErrGradeNotFound) {
			logger.Error("failed to load latest grade", "error", err)
			SetErrorFlash(s, "Failed to load assignment")
			redirectCoursePath(c, courseID)

			return
		}
		if latestGrade != nil {
			data["LatestGrade"] = makeGradeVerificationItem(*latestGrade)
		}
	}

	t.HTML(http.StatusOK, "assignment")
}

func makeAssignmentListItems(values []db.Assignment) []assignmentListItem {
	items := make([]assignmentListItem, 0, len(values))
	for _, value := range values {
		items = append(items, makeAssignmentListItem(value))
	}

	return items
}

func makeAssignmentListItem(value db.Assignment) assignmentListItem {
	return assignmentListItem{
		ID:           value.ID,
		CourseID:     value.CourseID,
		Title:        value.Title,
		Description:  value.Description,
		DueAt:        formatAssignmentDueAt(value.DueAt),
		DueAtInput:   formatDateTimeLocalInput(value.DueAt),
		MaxGrade:     value.MaxGrade,
		CreatedAt:    value.CreatedAt.Format("Jan 2, 2006"),
		MetadataHash: value.MetadataHash,
	}
}

func formatAssignmentDueAt(value *time.Time) string {
	if value == nil {
		return "No due date"
	}

	return value.Local().Format("Jan 2, 2006 15:04")
}

func formatDateTimeLocalInput(value *time.Time) string {
	if value == nil {
		return ""
	}

	return value.Local().Format("2006-01-02T15:04")
}

func parseOptionalDateTimeLocal(raw string) (*time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	parsed, err := time.ParseInLocation("2006-01-02T15:04", trimmed, time.Local)
	if err != nil {
		return nil, err
	}

	utc := parsed.UTC()

	return &utc, nil
}

func humanizeAssignmentWriteError(err error) string {
	switch {
	case errors.Is(err, db.ErrAssignmentTitleRequired):
		return db.ErrAssignmentTitleRequired.Error()
	case errors.Is(err, db.ErrAssignmentMaxGradeRequired):
		return db.ErrAssignmentMaxGradeRequired.Error()
	case errors.Is(err, db.ErrAssignmentMaxGradeInvalid):
		return db.ErrAssignmentMaxGradeInvalid.Error()
	case errors.Is(err, db.ErrOutlineItemNotFound):
		return "Choose a valid insertion point"
	case errors.Is(err, db.ErrAssignmentNotFound):
		return db.ErrAssignmentNotFound.Error()
	case errors.Is(err, db.ErrCourseNotFound):
		return db.ErrCourseNotFound.Error()
	case errors.Is(err, db.ErrTeacherRequired):
		return "Teacher access required"
	case errors.Is(err, db.ErrAccessDenied):
		return "You can only manage assignments for courses you teach"
	default:
		return "Failed to save assignment"
	}
}
