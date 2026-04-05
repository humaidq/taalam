/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package routes

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/flamego/flamego"
	"github.com/flamego/session"
	"github.com/flamego/template"

	"github.com/humaidq/taalam/db"
)

type submissionListItem struct {
	ID                 string
	AssignmentID       string
	StudentID          string
	StudentDisplayName string
	StudentUsername    string
	Version            int
	FileName           string
	FileSHA256         string
	SubmittedAt        string
}

type gradeVerificationItem struct {
	ID                       string
	SubmissionID             string
	StudentID                string
	Version                  int
	GradeValue               string
	FeedbackText             string
	CommitmentHash           string
	ComputedCommitmentHash   string
	PublishedAt              string
	BlockHeight              int64
	EventHash                string
	BlockHash                string
	EventType                string
	StoredCommitmentMatches  bool
	OnChainCommitmentMatches bool
}

// SubmissionGrade renders the grading page for one submission.
func SubmissionGrade(c flamego.Context, s session.Session, t template.Template, data template.Data) {
	setPage(data, "Grade Submission")
	data["IsCourses"] = true

	ctx := c.Request().Context()
	user, err := resolveSessionUser(ctx, s)
	if err != nil {
		SetErrorFlash(s, "Unable to resolve current user")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}
	if !user.Role.Satisfies(db.RoleTeacher) {
		SetErrorFlash(s, "Teacher access required")
		redirectCoursePath(c, c.Param("id"))

		return
	}

	courseID := strings.TrimSpace(c.Param("id"))
	assignmentID := strings.TrimSpace(c.Param("assignmentID"))
	submissionID := strings.TrimSpace(c.Param("submissionID"))

	canManage, err := db.CanUserManageAssignment(ctx, user.ID.String(), assignmentID)
	if err != nil {
		logger.Error("failed to check grading access", "error", err)
		SetErrorFlash(s, "Failed to load grade page")
		redirectAssignmentPath(c, courseID, assignmentID)

		return
	}
	if !canManage {
		SetErrorFlash(s, "Access restricted")
		redirectAssignmentPath(c, courseID, assignmentID)

		return
	}

	course, assignment, submission, err := loadGradePageContext(ctx, courseID, assignmentID, submissionID)
	if err != nil {
		handleGradePageLoadError(c, s, courseID, assignmentID, err)

		return
	}

	verification, err := db.GetGradeVerificationForAssignmentStudent(ctx, assignment.ID, submission.StudentID)
	if err != nil && !errors.Is(err, db.ErrGradeNotFound) {
		logger.Error("failed to load grade verification", "error", err)
		SetErrorFlash(s, "Failed to load grade page")
		redirectAssignmentPath(c, courseID, assignmentID)

		return
	}

	data["Course"] = makeCourseListItem(*course)
	data["Assignment"] = makeAssignmentListItem(*assignment)
	data["Submission"] = makeSubmissionListItem(submission)
	setBreadcrumbs(data, assignmentBreadcrumbs(data["Course"].(courseListItem), data["Assignment"].(assignmentListItem), "Grade"))
	data["CanPublishGrade"] = true
	if verification != nil {
		data["Grade"] = makeGradeVerificationItem(*verification)
	}

	t.HTML(http.StatusOK, "grade")
}

// StudentAssignmentGrade renders the latest grade verification page for the current student.
func StudentAssignmentGrade(c flamego.Context, s session.Session, t template.Template, data template.Data) {
	setPage(data, "Your Grade")
	data["IsCourses"] = true

	ctx := c.Request().Context()
	user, err := resolveSessionUser(ctx, s)
	if err != nil {
		SetErrorFlash(s, "Unable to resolve current user")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	courseID := strings.TrimSpace(c.Param("id"))
	assignmentID := strings.TrimSpace(c.Param("assignmentID"))
	if courseID == "" || assignmentID == "" {
		SetErrorFlash(s, "Missing assignment path")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	canView, err := db.CanUserViewAssignment(ctx, user.ID.String(), assignmentID)
	if err != nil {
		logger.Error("failed to check assignment access", "error", err)
		SetErrorFlash(s, "Failed to load grade page")
		redirectCoursePath(c, courseID)

		return
	}
	if !canView {
		SetErrorFlash(s, "Access restricted")
		redirectCoursePath(c, courseID)

		return
	}

	course, err := db.GetCourseByID(ctx, courseID)
	if err != nil {
		handleGradePageLoadError(c, s, courseID, assignmentID, err)

		return
	}
	assignment, err := db.GetAssignmentByID(ctx, assignmentID)
	if err != nil || assignment.CourseID != courseID {
		handleGradePageLoadError(c, s, courseID, assignmentID, db.ErrAssignmentNotFound)

		return
	}

	data["Course"] = makeCourseListItem(*course)
	data["Assignment"] = makeAssignmentListItem(*assignment)
	setBreadcrumbs(data, assignmentBreadcrumbs(data["Course"].(courseListItem), data["Assignment"].(assignmentListItem), "Grade"))
	data["CanPublishGrade"] = false

	verification, err := db.GetGradeVerificationForAssignmentStudent(ctx, assignmentID, user.ID.String())
	if err != nil {
		if errors.Is(err, db.ErrGradeNotFound) {
			t.HTML(http.StatusOK, "grade")

			return
		}

		logger.Error("failed to load grade verification", "error", err)
		SetErrorFlash(s, "Failed to load grade page")
		redirectAssignmentPath(c, courseID, assignmentID)

		return
	}

	submission, err := db.GetSubmissionWithStudentByID(ctx, verification.Grade.SubmissionID)
	if err == nil {
		data["Submission"] = makeSubmissionListItem(*submission)
	}
	data["Grade"] = makeGradeVerificationItem(*verification)

	t.HTML(http.StatusOK, "grade")
}

// PublishSubmissionGrade handles publishing or revising a grade.
func PublishSubmissionGrade(c flamego.Context, s session.Session) {
	if err := c.Request().ParseForm(); err != nil {
		SetErrorFlash(s, "Failed to parse form")
		redirectSubmissionGradePath(c, c.Param("id"), c.Param("assignmentID"), c.Param("submissionID"))

		return
	}

	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		redirectSubmissionGradePath(c, c.Param("id"), c.Param("assignmentID"), c.Param("submissionID"))

		return
	}

	verification, err := db.PublishGrade(c.Request().Context(), db.PublishGradeInput{
		SubmissionID: c.Param("submissionID"),
		PublishedBy:  userID,
		GradeValue:   c.Request().Form.Get("grade_value"),
		FeedbackText: c.Request().Form.Get("feedback_text"),
	})
	if err != nil {
		SetErrorFlash(s, humanizeGradeWriteError(err))
		redirectSubmissionGradePath(c, c.Param("id"), c.Param("assignmentID"), c.Param("submissionID"))

		return
	}

	message := "Grade published"
	if verification.Grade.Version > 1 {
		message = "Grade revised"
	}
	SetSuccessFlash(s, message)
	redirectSubmissionGradePath(c, c.Param("id"), c.Param("assignmentID"), c.Param("submissionID"))
}

func loadGradePageContext(ctx context.Context, courseID string, assignmentID string, submissionID string) (*db.Course, *db.Assignment, db.SubmissionWithStudent, error) {
	course, err := db.GetCourseByID(ctx, courseID)
	if err != nil {
		return nil, nil, db.SubmissionWithStudent{}, err
	}
	assignment, err := db.GetAssignmentByID(ctx, assignmentID)
	if err != nil {
		return nil, nil, db.SubmissionWithStudent{}, err
	}
	if assignment.CourseID != courseID {
		return nil, nil, db.SubmissionWithStudent{}, db.ErrAssignmentNotFound
	}
	submission, err := db.GetSubmissionWithStudentByID(ctx, submissionID)
	if err != nil {
		return nil, nil, db.SubmissionWithStudent{}, err
	}
	if submission.AssignmentID != assignmentID {
		return nil, nil, db.SubmissionWithStudent{}, db.ErrSubmissionNotFound
	}

	return course, assignment, *submission, nil
}

func handleGradePageLoadError(c flamego.Context, s session.Session, courseID string, assignmentID string, err error) {
	switch {
	case errors.Is(err, db.ErrCourseNotFound):
		SetErrorFlash(s, "Course not found")
		c.Redirect("/courses", http.StatusSeeOther)
	case errors.Is(err, db.ErrAssignmentNotFound):
		SetErrorFlash(s, "Assignment not found")
		redirectCoursePath(c, courseID)
	case errors.Is(err, db.ErrSubmissionNotFound):
		SetErrorFlash(s, "Submission not found")
		redirectAssignmentPath(c, courseID, assignmentID)
	default:
		SetErrorFlash(s, "Failed to load grade page")
		redirectAssignmentPath(c, courseID, assignmentID)
	}
}

func makeSubmissionListItems(values []db.SubmissionWithStudent) []submissionListItem {
	items := make([]submissionListItem, 0, len(values))
	for _, value := range values {
		items = append(items, makeSubmissionListItem(value))
	}

	return items
}

func makeSubmissionListItem(value db.SubmissionWithStudent) submissionListItem {
	return submissionListItem{
		ID:                 value.ID,
		AssignmentID:       value.AssignmentID,
		StudentID:          value.StudentID,
		StudentDisplayName: value.StudentDisplayName,
		StudentUsername:    valueOrEmpty(value.StudentUsername),
		Version:            value.Version,
		FileName:           value.FileName,
		FileSHA256:         value.FileSHA256,
		SubmittedAt:        value.SubmittedAt.Format("Jan 2, 2006 15:04:05 MST"),
	}
}

func makeGradeVerificationItem(value db.GradeVerification) gradeVerificationItem {
	return gradeVerificationItem{
		ID:                       value.Grade.ID,
		SubmissionID:             value.Grade.SubmissionID,
		StudentID:                value.Grade.StudentID,
		Version:                  value.Grade.Version,
		GradeValue:               value.Grade.GradeValue,
		FeedbackText:             value.Grade.FeedbackText,
		CommitmentHash:           value.Grade.CommitmentHash,
		ComputedCommitmentHash:   value.ComputedCommitmentHash,
		PublishedAt:              value.Grade.PublishedAt.Format("Jan 2, 2006 15:04:05 MST"),
		BlockHeight:              value.BlockHeight,
		EventHash:                value.EventHash,
		BlockHash:                value.BlockHash,
		EventType:                value.EventType,
		StoredCommitmentMatches:  value.StoredCommitmentMatches,
		OnChainCommitmentMatches: value.OnChainCommitmentMatches,
	}
}

func redirectSubmissionGradePath(c flamego.Context, courseID string, assignmentID string, submissionID string) {
	if strings.TrimSpace(courseID) == "" || strings.TrimSpace(assignmentID) == "" || strings.TrimSpace(submissionID) == "" {
		redirectAssignmentPath(c, courseID, assignmentID)

		return
	}

	c.Redirect("/courses/"+courseID+"/assignments/"+assignmentID+"/submissions/"+submissionID+"/grade", http.StatusSeeOther)
}

func humanizeGradeWriteError(err error) string {
	switch {
	case errors.Is(err, db.ErrSubmissionNotFound):
		return db.ErrSubmissionNotFound.Error()
	case errors.Is(err, db.ErrAssignmentNotFound):
		return db.ErrAssignmentNotFound.Error()
	case errors.Is(err, db.ErrTeacherRequired):
		return "Teacher access required"
	case errors.Is(err, db.ErrAccessDenied):
		return "You can only grade assignments for courses you manage"
	case errors.Is(err, db.ErrGradeValueRequired):
		return db.ErrGradeValueRequired.Error()
	case errors.Is(err, db.ErrGradeValueInvalid):
		return db.ErrGradeValueInvalid.Error()
	case errors.Is(err, db.ErrGradeExceedsAssignmentMaximum):
		return db.ErrGradeExceedsAssignmentMaximum.Error()
	default:
		return "Failed to publish grade"
	}
}

func makeCourseListItem(course db.Course) courseListItem {
	return courseListItem{
		ID:              course.ID,
		Code:            course.Code,
		Title:           course.Title,
		Term:            course.Term,
		InstructorCount: course.InstructorCount,
		StudentCount:    course.StudentCount,
		CreatedAt:       course.CreatedAt.Format("Jan 2, 2006"),
	}
}
