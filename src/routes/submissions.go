/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package routes

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/flamego/flamego"
	"github.com/flamego/session"
	"github.com/flamego/template"

	"github.com/humaidq/taalam/db"
)

type submissionReceiptItem struct {
	ID           string
	AssignmentID string
	Version      int
	FileName     string
	ContentType  string
	FileSize     int64
	FileSHA256   string
	SubmittedAt  string
	BlockHeight  int64
	EventHash    string
	BlockHash    string
}

// CreateSubmission handles student submission uploads.
func CreateSubmission(c flamego.Context, s session.Session) {
	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		redirectAssignmentPath(c, c.Param("id"), c.Param("assignmentID"))

		return
	}

	const requestLimit = db.MaxSubmissionFileSizeBytes + (1 << 20)
	req := c.Request().Request
	req.Body = http.MaxBytesReader(c.ResponseWriter(), req.Body, requestLimit)
	if err := req.ParseMultipartForm(requestLimit); err != nil {
		SetErrorFlash(s, humanizeSubmissionWriteError(db.ErrSubmissionFileTooLarge))
		redirectAssignmentPath(c, c.Param("id"), c.Param("assignmentID"))

		return
	}

	file, header, err := req.FormFile("submission_file")
	if err != nil {
		SetErrorFlash(s, humanizeSubmissionWriteError(db.ErrSubmissionFileRequired))
		redirectAssignmentPath(c, c.Param("id"), c.Param("assignmentID"))

		return
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		SetErrorFlash(s, "Failed to read uploaded file")
		redirectAssignmentPath(c, c.Param("id"), c.Param("assignmentID"))

		return
	}

	receipt, err := db.CreateSubmission(c.Request().Context(), db.CreateSubmissionInput{
		AssignmentID: c.Param("assignmentID"),
		StudentID:    userID,
		FileName:     header.Filename,
		ContentType:  header.Header.Get("Content-Type"),
		FileBytes:    fileBytes,
	})
	if err != nil {
		SetErrorFlash(s, humanizeSubmissionWriteError(err))
		redirectAssignmentPath(c, c.Param("id"), c.Param("assignmentID"))

		return
	}

	SetSuccessFlash(s, "Submission committed")
	redirectSubmissionReceiptPath(c, c.Param("id"), c.Param("assignmentID"), receipt.Submission.ID)
}

// SubmissionReceipt renders a receipt page for a submission.
func SubmissionReceipt(c flamego.Context, s session.Session, t template.Template, data template.Data) {
	setPage(data, "Submission Receipt")
	data["IsCourses"] = true

	ctx := c.Request().Context()
	user, err := resolveSessionUser(ctx, s)
	if err != nil {
		SetErrorFlash(s, "Unable to resolve current user")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	submissionID := strings.TrimSpace(c.Param("submissionID"))
	assignmentID := strings.TrimSpace(c.Param("assignmentID"))
	courseID := strings.TrimSpace(c.Param("id"))
	if submissionID == "" || assignmentID == "" || courseID == "" {
		SetErrorFlash(s, "Missing submission path")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	canView, err := db.CanUserViewSubmission(ctx, user.ID.String(), submissionID)
	if err != nil {
		logger.Error("failed to check submission visibility", "error", err)
		SetErrorFlash(s, "Failed to load submission")
		redirectAssignmentPath(c, courseID, assignmentID)

		return
	}
	if !canView {
		SetErrorFlash(s, "Access restricted")
		redirectAssignmentPath(c, courseID, assignmentID)

		return
	}

	receipt, err := db.GetSubmissionReceipt(ctx, submissionID)
	if err != nil {
		if errors.Is(err, db.ErrSubmissionNotFound) {
			SetErrorFlash(s, "Submission not found")
			redirectAssignmentPath(c, courseID, assignmentID)

			return
		}

		logger.Error("failed to load submission receipt", "error", err)
		SetErrorFlash(s, "Failed to load submission")
		redirectAssignmentPath(c, courseID, assignmentID)

		return
	}

	assignment, err := db.GetAssignmentByID(ctx, assignmentID)
	if err != nil {
		SetErrorFlash(s, "Assignment not found")
		redirectCoursePath(c, courseID)

		return
	}
	if assignment.ID != receipt.Submission.AssignmentID || assignment.CourseID != courseID {
		SetErrorFlash(s, "Submission not found")
		redirectAssignmentPath(c, courseID, assignmentID)

		return
	}

	course, err := db.GetCourseByID(ctx, courseID)
	if err != nil {
		SetErrorFlash(s, "Course not found")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	data["Course"] = courseListItem{
		ID:              course.ID,
		Code:            course.Code,
		Title:           course.Title,
		Term:            course.Term,
		InstructorCount: course.InstructorCount,
		StudentCount:    course.StudentCount,
		CreatedAt:       course.CreatedAt.Format("Jan 2, 2006"),
	}
	data["Assignment"] = makeAssignmentListItem(*assignment)
	setBreadcrumbs(data, assignmentBreadcrumbs(data["Course"].(courseListItem), data["Assignment"].(assignmentListItem), "Submission Receipt"))
	data["Receipt"] = submissionReceiptItem{
		ID:           receipt.Submission.ID,
		AssignmentID: receipt.Submission.AssignmentID,
		Version:      receipt.Submission.Version,
		FileName:     receipt.Submission.FileName,
		ContentType:  receipt.Submission.ContentType,
		FileSize:     receipt.Submission.FileSize,
		FileSHA256:   receipt.Submission.FileSHA256,
		SubmittedAt:  receipt.Submission.SubmittedAt.Format("Jan 2, 2006 15:04:05 MST"),
		BlockHeight:  receipt.BlockHeight,
		EventHash:    receipt.EventHash,
		BlockHash:    receipt.BlockHash,
	}

	t.HTML(http.StatusOK, "submission_receipt")
}

func redirectAssignmentPath(c flamego.Context, courseID string, assignmentID string) {
	if strings.TrimSpace(courseID) == "" || strings.TrimSpace(assignmentID) == "" {
		redirectCoursePath(c, courseID)

		return
	}

	c.Redirect("/courses/"+courseID+"/assignments/"+assignmentID, http.StatusSeeOther)
}

func redirectSubmissionReceiptPath(c flamego.Context, courseID string, assignmentID string, submissionID string) {
	if strings.TrimSpace(courseID) == "" || strings.TrimSpace(assignmentID) == "" || strings.TrimSpace(submissionID) == "" {
		redirectAssignmentPath(c, courseID, assignmentID)

		return
	}

	c.Redirect("/courses/"+courseID+"/assignments/"+assignmentID+"/submissions/"+submissionID, http.StatusSeeOther)
}

func humanizeSubmissionWriteError(err error) string {
	switch {
	case errors.Is(err, db.ErrSubmissionFileRequired):
		return db.ErrSubmissionFileRequired.Error()
	case errors.Is(err, db.ErrSubmissionFileNameRequired):
		return db.ErrSubmissionFileNameRequired.Error()
	case errors.Is(err, db.ErrSubmissionFileTooLarge):
		return "Submission file is too large. Keep uploads under 10 MiB."
	case errors.Is(err, db.ErrSubmissionPastDue):
		return "This assignment is past due and no longer accepts submissions"
	case errors.Is(err, db.ErrAssignmentNotFound):
		return db.ErrAssignmentNotFound.Error()
	case errors.Is(err, db.ErrStudentRequired):
		return "Student access required"
	case errors.Is(err, db.ErrAccessDenied):
		return "You can only submit to assignments for courses you are enrolled in"
	default:
		return "Failed to save submission"
	}
}
