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

type courseListItem struct {
	ID              string
	Code            string
	Title           string
	Term            string
	InstructorCount int
	StudentCount    int
	CreatedAt       string
}

type courseUserItem struct {
	ID          string
	DisplayName string
	Username    string
	RoleLabel   string
	AssignedAt  string
}

type coursePersonOptionItem struct {
	ID          string
	DisplayName string
	Username    string
}

type courseCompletionItem struct {
	ID                 string
	StudentID          string
	StudentDisplayName string
	StudentUsername    string
	Status             string
	CompletedAt        string
	CertificateID      string
	CertificateCode    string
	HasCertificate     bool
}

type courseCertificateItem struct {
	ID                 string
	StudentDisplayName string
	CertificateCode    string
	ResultSummary      string
	GradeSummary       string
	IssuedAt           string
	RevokedAt          string
	IsRevoked          bool
}

// NewCourseForm renders the course creation page.
func NewCourseForm(t template.Template, data template.Data) {
	setPage(data, "New Course")
	setBreadcrumbs(data, []BreadcrumbItem{
		homeBreadcrumb(),
		{Name: "Courses", URL: "/courses"},
		{Name: "New Course", IsCurrent: true},
	})
	data["IsCourses"] = true
	t.HTML(http.StatusOK, "course_new")
}

// Courses renders the courses page for the current user.
func Courses(c flamego.Context, s session.Session, t template.Template, data template.Data) {
	setPage(data, "Courses")
	setBreadcrumbs(data, coursesBreadcrumbs())
	data["IsCourses"] = true

	ctx := c.Request().Context()
	user, err := resolveSessionUser(ctx, s)
	if err != nil {
		SetErrorFlash(s, "Unable to resolve current user")
		c.Redirect("/", http.StatusSeeOther)

		return
	}

	courses, err := db.ListCoursesForRole(ctx, user.ID.String(), user.Role)
	if err != nil {
		logger.Error("failed to list courses", "error", err)
		data["Error"] = "Failed to load courses"
		t.HTML(http.StatusInternalServerError, "courses")

		return
	}

	items := make([]courseListItem, 0, len(courses))
	for _, course := range courses {
		items = append(items, courseListItem{
			ID:              course.ID,
			Code:            course.Code,
			Title:           course.Title,
			Term:            course.Term,
			InstructorCount: course.InstructorCount,
			StudentCount:    course.StudentCount,
			CreatedAt:       course.CreatedAt.Format("Jan 2, 2006"),
		})
	}

	data["Courses"] = items
	data["CourseCount"] = len(items)
	data["CanCreateCourse"] = user.Role.IsAdmin()

	t.HTML(http.StatusOK, "courses")
}

// CourseDetail renders a single course page.
func CourseDetail(c flamego.Context, s session.Session, t template.Template, data template.Data) {
	setPage(data, "Course")
	data["IsCourses"] = true

	ctx := c.Request().Context()
	user, err := resolveSessionUser(ctx, s)
	if err != nil {
		SetErrorFlash(s, "Unable to resolve current user")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	courseID := strings.TrimSpace(c.Param("id"))
	if courseID == "" {
		SetErrorFlash(s, "Missing course ID")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	canView, err := db.CanUserViewCourse(ctx, user.ID.String(), courseID)
	if err != nil {
		logger.Error("failed to check course visibility", "error", err)
		SetErrorFlash(s, "Failed to load course")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}
	if !canView {
		SetErrorFlash(s, "Access restricted")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	course, err := db.GetCourseByID(ctx, courseID)
	if err != nil {
		if errors.Is(err, db.ErrCourseNotFound) {
			SetErrorFlash(s, "Course not found")
			c.Redirect("/courses", http.StatusSeeOther)

			return
		}

		logger.Error("failed to get course", "error", err)
		SetErrorFlash(s, "Failed to load course")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	canManage, err := db.CanUserManageCourse(ctx, user.ID.String(), courseID)
	if err != nil {
		logger.Error("failed to check course management access", "error", err)
		SetErrorFlash(s, "Failed to load course")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	instructors, err := db.ListCourseInstructors(ctx, courseID)
	if err != nil {
		logger.Error("failed to list course instructors", "error", err)
		data["Error"] = "Failed to load course roster"
		t.HTML(http.StatusInternalServerError, "course")

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
	setBreadcrumbs(data, courseBreadcrumbs(data["Course"].(courseListItem)))
	data["CanManageCourse"] = canManage
	data["CanAssignCourse"] = user.Role.IsAdmin()
	data["CanCreateAssignment"] = canManage
	data["Instructors"] = makeCourseUserItems(instructors)

	assignments, err := db.ListAssignmentsForCourse(ctx, courseID)
	if err != nil {
		logger.Error("failed to list assignments", "error", err)
		data["Error"] = "Failed to load assignments"
		t.HTML(http.StatusInternalServerError, "course")

		return
	}

	data["Assignments"] = makeAssignmentListItems(assignments)

	studentID := ""
	if user.Role.IsStudent() {
		studentID = user.ID.String()
	}

	outlineItems, err := loadCourseOutlineItemsForStudent(ctx, courseID, studentID)
	if err != nil {
		logger.Error("failed to load course outline", "error", err)
		data["Error"] = "Failed to load course content"
		t.HTML(http.StatusInternalServerError, "course")

		return
	}

	data["CourseOutline"] = outlineItems

	if canManage {
		students, err := db.ListCourseStudents(ctx, courseID)
		if err != nil {
			logger.Error("failed to list course students", "error", err)
			data["Error"] = "Failed to load course roster"
			t.HTML(http.StatusInternalServerError, "course")

			return
		}

		data["Students"] = makeCourseUserItems(students)

		completions, err := db.ListCourseCompletions(ctx, courseID)
		if err != nil {
			logger.Error("failed to list course completions", "error", err)
			data["Error"] = "Failed to load course completion records"
			t.HTML(http.StatusInternalServerError, "course")

			return
		}

		data["Completions"] = makeCourseCompletionItems(completions)
		data["CanMarkCompletion"] = true
	}

	if canManage {
		certificates, err := db.ListCourseCertificates(ctx, courseID)
		if err != nil {
			logger.Error("failed to list course certificates", "error", err)
			data["Error"] = "Failed to load certificates"
			t.HTML(http.StatusInternalServerError, "course")

			return
		}

		certificateItems := makeCourseCertificateItems(certificates)
		data["Certificates"] = certificateItems
	}

	if user.Role.IsAdmin() {
		availableTeachers, err := db.ListAvailableTeachers(ctx, courseID)
		if err != nil {
			logger.Error("failed to list available teachers", "error", err)
			data["Error"] = "Failed to load course assignment options"
			t.HTML(http.StatusInternalServerError, "course")

			return
		}

		availableStudents, err := db.ListAvailableStudents(ctx, courseID)
		if err != nil {
			logger.Error("failed to list available students", "error", err)
			data["Error"] = "Failed to load enrollment options"
			t.HTML(http.StatusInternalServerError, "course")

			return
		}

		data["AvailableTeachers"] = makeCoursePersonOptionItems(availableTeachers)
		data["AvailableStudents"] = makeCoursePersonOptionItems(availableStudents)
		data["CanIssueCertificate"] = true
		data["CanRevokeCertificate"] = true
		data["CertificateCandidates"] = filterCertificateCandidateItems(valueOrCourseCompletionSlice(data["Completions"]))
	}

	t.HTML(http.StatusOK, "course")
}

// CreateCourse handles course creation.
func CreateCourse(c flamego.Context, s session.Session) {
	if err := c.Request().ParseForm(); err != nil {
		SetErrorFlash(s, "Failed to parse form")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	course, err := db.CreateCourse(c.Request().Context(), db.CreateCourseInput{
		Code:      c.Request().Form.Get("code"),
		Title:     c.Request().Form.Get("title"),
		Term:      c.Request().Form.Get("term"),
		CreatedBy: userID,
	})
	if err != nil {
		SetErrorFlash(s, humanizeCourseWriteError(err))
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	SetSuccessFlash(s, "Course created")
	c.Redirect("/courses/"+course.ID, http.StatusSeeOther)
}

// UpdateCourse handles course updates.
func UpdateCourse(c flamego.Context, s session.Session) {
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

	course, err := db.UpdateCourse(c.Request().Context(), db.UpdateCourseInput{
		CourseID:  c.Param("id"),
		Code:      c.Request().Form.Get("code"),
		Title:     c.Request().Form.Get("title"),
		Term:      c.Request().Form.Get("term"),
		UpdatedBy: userID,
	})
	if err != nil {
		SetErrorFlash(s, humanizeCourseWriteError(err))
		redirectCoursePath(c, c.Param("id"))

		return
	}

	SetSuccessFlash(s, "Course updated")
	c.Redirect("/courses/"+course.ID, http.StatusSeeOther)
}

// DeleteCourse removes a course.
func DeleteCourse(c flamego.Context, s session.Session) {
	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		redirectCoursePath(c, c.Param("id"))

		return
	}

	if err := db.DeleteCourse(c.Request().Context(), c.Param("id"), userID); err != nil {
		SetErrorFlash(s, humanizeCourseWriteError(err))
		redirectCoursePath(c, c.Param("id"))

		return
	}

	SetSuccessFlash(s, "Course deleted")
	c.Redirect("/courses", http.StatusSeeOther)
}

// AssignCourseInstructor handles teacher assignment.
func AssignCourseInstructor(c flamego.Context, s session.Session) {
	if err := c.Request().ParseForm(); err != nil {
		SetErrorFlash(s, "Failed to parse form")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	courseID := strings.TrimSpace(c.Param("id"))
	err := db.AssignCourseInstructor(c.Request().Context(), db.AssignCourseInstructorInput{
		CourseID:   courseID,
		TeacherID:  c.Request().Form.Get("teacher_id"),
		AssignedBy: userID,
	})
	if err != nil {
		SetErrorFlash(s, humanizeCourseWriteError(err))
		redirectCoursePath(c, courseID)

		return
	}

	SetSuccessFlash(s, "Instructor assigned")
	redirectCoursePath(c, courseID)
}

// EnrollCourseStudent handles student enrollment.
func EnrollCourseStudent(c flamego.Context, s session.Session) {
	if err := c.Request().ParseForm(); err != nil {
		SetErrorFlash(s, "Failed to parse form")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	courseID := strings.TrimSpace(c.Param("id"))
	err := db.EnrollCourseStudent(c.Request().Context(), db.EnrollCourseStudentInput{
		CourseID:   courseID,
		StudentID:  c.Request().Form.Get("student_id"),
		EnrolledBy: userID,
	})
	if err != nil {
		SetErrorFlash(s, humanizeCourseWriteError(err))
		redirectCoursePath(c, courseID)

		return
	}

	SetSuccessFlash(s, "Student enrolled")
	redirectCoursePath(c, courseID)
}

func makeCourseUserItems(values []db.CourseUser) []courseUserItem {
	items := make([]courseUserItem, 0, len(values))
	for _, value := range values {
		items = append(items, courseUserItem{
			ID:          value.ID,
			DisplayName: value.DisplayName,
			Username:    valueOrEmpty(value.Username),
			RoleLabel:   value.Role.Label(),
			AssignedAt:  value.AssignedAt.Format("Jan 2, 2006"),
		})
	}

	return items
}

func makeCoursePersonOptionItems(values []db.CoursePersonOption) []coursePersonOptionItem {
	items := make([]coursePersonOptionItem, 0, len(values))
	for _, value := range values {
		items = append(items, coursePersonOptionItem{
			ID:          value.ID,
			DisplayName: value.DisplayName,
			Username:    valueOrEmpty(value.Username),
		})
	}

	return items
}

func makeCourseCompletionItems(values []db.CourseCompletion) []courseCompletionItem {
	items := make([]courseCompletionItem, 0, len(values))
	for _, value := range values {
		item := courseCompletionItem{
			ID:                 value.ID,
			StudentID:          value.StudentID,
			StudentDisplayName: value.StudentDisplayName,
			StudentUsername:    valueOrEmpty(value.StudentUsername),
			Status:             value.Status,
			CompletedAt:        value.CompletedAt.Format("Jan 2, 2006 15:04"),
		}
		if value.CertificateID != nil {
			item.CertificateID = *value.CertificateID
			item.HasCertificate = true
		}
		if value.CertificateCode != nil {
			item.CertificateCode = *value.CertificateCode
		}

		items = append(items, item)
	}

	return items
}

func makeCourseCertificateItems(values []db.CertificateRecord) []courseCertificateItem {
	items := make([]courseCertificateItem, 0, len(values))
	for _, value := range values {
		item := courseCertificateItem{
			ID:                 value.ID,
			StudentDisplayName: value.StudentDisplayName,
			CertificateCode:    value.CertificateCode,
			ResultSummary:      value.ResultSummary,
			GradeSummary:       value.GradeSummary,
			IssuedAt:           value.IssuedAt.Format("Jan 2, 2006 15:04"),
			IsRevoked:          value.RevokedAt != nil,
		}
		if value.RevokedAt != nil {
			item.RevokedAt = value.RevokedAt.Format("Jan 2, 2006 15:04")
		}

		items = append(items, item)
	}

	return items
}

func filterCertificateCandidateItems(values []courseCompletionItem) []courseCompletionItem {
	items := make([]courseCompletionItem, 0)
	for _, value := range values {
		if value.Status != "completed" || value.HasCertificate {
			continue
		}

		items = append(items, value)
	}

	return items
}

func valueOrCourseCompletionSlice(value any) []courseCompletionItem {
	items, ok := value.([]courseCompletionItem)
	if !ok {
		return nil
	}

	return items
}

func redirectCoursePath(c flamego.Context, courseID string) {
	if strings.TrimSpace(courseID) == "" {
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	c.Redirect("/courses/"+courseID, http.StatusSeeOther)
}

func humanizeCourseWriteError(err error) string {
	switch {
	case errors.Is(err, db.ErrCourseCodeRequired):
		return db.ErrCourseCodeRequired.Error()
	case errors.Is(err, db.ErrCourseTitleRequired):
		return db.ErrCourseTitleRequired.Error()
	case errors.Is(err, db.ErrCourseTermRequired):
		return db.ErrCourseTermRequired.Error()
	case errors.Is(err, db.ErrCourseAlreadyExists):
		return db.ErrCourseAlreadyExists.Error()
	case errors.Is(err, db.ErrCourseNotFound):
		return db.ErrCourseNotFound.Error()
	case errors.Is(err, db.ErrCourseInstructorAlreadyAssigned):
		return db.ErrCourseInstructorAlreadyAssigned.Error()
	case errors.Is(err, db.ErrCourseStudentAlreadyEnrolled):
		return db.ErrCourseStudentAlreadyEnrolled.Error()
	case errors.Is(err, db.ErrTeacherRequired):
		return "Choose a valid teacher account"
	case errors.Is(err, db.ErrStudentRequired):
		return "Choose a valid student account"
	case errors.Is(err, db.ErrAdminRequired):
		return "Admin access required"
	case errors.Is(err, db.ErrUserNotFound):
		return "User not found"
	default:
		return "Failed to save course changes"
	}
}
