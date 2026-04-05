/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package routes

import (
	"errors"
	"net/http"
	"strings"

	"github.com/flamego/flamego"
	"github.com/flamego/session"
	"github.com/flamego/template"

	"github.com/humaidq/taalam/db"
)

type certificateVerificationItem struct {
	CertificateCode    string
	StudentDisplayName string
	CourseCode         string
	CourseTitle        string
	ResultSummary      string
	GradeSummary       string
	CertificateHash    string
	ComputedHash       string
	IssuedAt           string
	Revoked            bool
	RevokedAt          string
	HashMatches        bool
	LegacyCompatible   bool
	StudentClaim       string
	StudentMatches     bool
	CourseClaim        string
	CourseMatches      bool
	ResultClaim        string
	ResultMatches      bool
	GradeClaim         string
	GradeMatches       bool
}

// MarkCourseCompletion handles completion marking.
func MarkCourseCompletion(c flamego.Context, s session.Session) {
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

	_, err := db.MarkCourseCompletion(c.Request().Context(), db.MarkCourseCompletionInput{
		CourseID:  c.Param("id"),
		StudentID: c.Request().Form.Get("student_id"),
		MarkedBy:  userID,
	})
	if err != nil {
		SetErrorFlash(s, humanizeCertificateWriteError(err))
		redirectCoursePath(c, c.Param("id"))

		return
	}

	SetSuccessFlash(s, "Course completion recorded")
	redirectCoursePath(c, c.Param("id"))
}

// IssueCertificate handles certificate issuance.
func IssueCertificate(c flamego.Context, s session.Session) {
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

	certificate, err := db.IssueCertificate(c.Request().Context(), db.IssueCertificateInput{
		CourseID:  c.Param("id"),
		StudentID: c.Request().Form.Get("student_id"),
		IssuedBy:  userID,
	})
	if err != nil {
		SetErrorFlash(s, humanizeCertificateWriteError(err))
		redirectCoursePath(c, c.Param("id"))

		return
	}

	SetSuccessFlash(s, "Certificate issued")
	c.Redirect("/certificates/verify?code="+certificate.CertificateCode, http.StatusSeeOther)
}

// RevokeCertificate handles certificate revocation.
func RevokeCertificate(c flamego.Context, s session.Session) {
	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		redirectCoursePath(c, c.Param("id"))

		return
	}

	_, err := db.RevokeCertificate(c.Request().Context(), db.RevokeCertificateInput{
		CertificateID: c.Param("certificateID"),
		RevokedBy:     userID,
	})
	if err != nil {
		SetErrorFlash(s, humanizeCertificateWriteError(err))
		redirectCoursePath(c, c.Param("id"))

		return
	}

	SetSuccessFlash(s, "Certificate revoked")
	redirectCoursePath(c, c.Param("id"))
}

// CertificateVerify renders the public certificate verification page.
func CertificateVerify(c flamego.Context, t template.Template, data template.Data) {
	setPage(data, "Verify Certificate")
	setBreadcrumbs(data, []BreadcrumbItem{{Name: "Verify Certificate", IsCurrent: true}})
	data["HeaderOnly"] = false
	data["CertificateCode"] = strings.TrimSpace(c.Query("code"))
	data["ClaimStudent"] = strings.TrimSpace(c.Query("student"))
	data["ClaimCourse"] = strings.TrimSpace(c.Query("course"))
	data["ClaimResult"] = strings.TrimSpace(c.Query("result"))
	data["ClaimGrade"] = strings.TrimSpace(c.Query("grade"))

	code := strings.TrimSpace(c.Query("code"))
	if code == "" {
		t.HTML(http.StatusOK, "certificate_verify")

		return
	}

	verification, err := db.VerifyCertificateClaims(
		c.Request().Context(),
		code,
		strings.TrimSpace(c.Query("student")),
		strings.TrimSpace(c.Query("course")),
		strings.TrimSpace(c.Query("result")),
		strings.TrimSpace(c.Query("grade")),
	)
	if err != nil {
		if errors.Is(err, db.ErrCertificateNotFound) {
			data["VerificationError"] = "Certificate not found"
			t.HTML(http.StatusNotFound, "certificate_verify")

			return
		}

		data["VerificationError"] = "Failed to verify certificate"
		t.HTML(http.StatusInternalServerError, "certificate_verify")

		return
	}

	item := certificateVerificationItem{
		CertificateCode:    verification.Certificate.CertificateCode,
		StudentDisplayName: verification.Certificate.StudentDisplayName,
		CourseCode:         verification.Certificate.CourseCode,
		CourseTitle:        verification.Certificate.CourseTitle,
		ResultSummary:      verification.Certificate.ResultSummary,
		GradeSummary:       verification.Certificate.GradeSummary,
		CertificateHash:    verification.Certificate.CertificateHash,
		ComputedHash:       verification.ComputedHash,
		IssuedAt:           verification.Certificate.IssuedAt.Format("Jan 2, 2006 15:04 MST"),
		Revoked:            verification.Revoked,
		HashMatches:        verification.HashMatches,
		LegacyCompatible:   verification.LegacyCompatible,
		StudentClaim:       verification.StudentClaim,
		StudentMatches:     verification.StudentMatches,
		CourseClaim:        verification.CourseClaim,
		CourseMatches:      verification.CourseMatches,
		ResultClaim:        verification.ResultClaim,
		ResultMatches:      verification.ResultMatches,
		GradeClaim:         verification.GradeClaim,
		GradeMatches:       verification.GradeMatches,
	}
	if verification.Certificate.RevokedAt != nil {
		item.RevokedAt = verification.Certificate.RevokedAt.Format("Jan 2, 2006 15:04 MST")
	}
	data["Verification"] = item

	t.HTML(http.StatusOK, "certificate_verify")
}

func humanizeCertificateWriteError(err error) string {
	switch {
	case errors.Is(err, db.ErrCourseNotFound):
		return db.ErrCourseNotFound.Error()
	case errors.Is(err, db.ErrUserNotFound):
		return db.ErrUserNotFound.Error()
	case errors.Is(err, db.ErrTeacherRequired):
		return "Teacher access required"
	case errors.Is(err, db.ErrAdminRequired):
		return "Admin access required"
	case errors.Is(err, db.ErrStudentRequired):
		return "Choose a valid student account"
	case errors.Is(err, db.ErrAccessDenied):
		return "You do not have access to manage this course"
	case errors.Is(err, db.ErrCourseCompletionRequired):
		return db.ErrCourseCompletionRequired.Error()
	case errors.Is(err, db.ErrCourseAlreadyCertified):
		return db.ErrCourseAlreadyCertified.Error()
	case errors.Is(err, db.ErrCertificateNotFound):
		return db.ErrCertificateNotFound.Error()
	default:
		return "Failed to save certificate changes"
	}
}
