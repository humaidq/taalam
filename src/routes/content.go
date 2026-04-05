/*
 * Copyright 2026 Humaid Alqasimi
 * SPDX-License-Identifier: Apache-2.0
 */
package routes

import (
	"bytes"
	"context"
	htmltemplate "html/template"
	"net/http"
	"strings"

	"github.com/flamego/flamego"
	"github.com/flamego/session"
	"github.com/flamego/template"
	"github.com/yuin/goldmark"

	"github.com/humaidq/taalam/db"
)

var lessonMarkdownRenderer = goldmark.New()

type courseOutlineItemView struct {
	ID         string
	Position   int
	ItemType   string
	Unit       *courseUnitContentItem
	Assignment *assignmentListItem
}

type courseUnitContentItem struct {
	ID          string
	Title       string
	Description string
	LessonCount int
	Lessons     []courseLessonItem
}

type courseLessonItem struct {
	ID          string
	UnitID      string
	Title       string
	Description string
	Position    int
	SlideCount  int
	UpdatedAt   string
	Completed   bool
}

type outlineInsertOptionItem struct {
	ID    string
	Label string
}

type lessonSlideView struct {
	ID           string
	Title        string
	MarkdownRaw  string
	RenderedHTML htmltemplate.HTML
	Position     int
	UpdatedAt    string
}

type lessonUnitItem struct {
	ID    string
	Title string
}

// NewUnitForm renders the unit creation page for a course.
func NewUnitForm(c flamego.Context, s session.Session, t template.Template, data template.Data) {
	setPage(data, "New Unit")
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
		{Name: "New Unit", IsCurrent: true},
	})
	data["OutlineInsertOptions"] = buildOutlineInsertOptions(outlineItems)
	t.HTML(http.StatusOK, "unit_new")
}

// NewLessonForm renders the lesson creation page for a unit.
func NewLessonForm(c flamego.Context, s session.Session, t template.Template, data template.Data) {
	setPage(data, "New Lesson")
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

	unitID := strings.TrimSpace(c.Param("unitID"))
	unit, err := db.GetCourseUnitByID(ctx, unitID)
	if err != nil || unit.CourseID != courseID {
		SetErrorFlash(s, "Unit not found")
		redirectCoursePath(c, courseID)

		return
	}

	data["Course"] = makeCourseListItem(*course)
	data["Unit"] = lessonUnitItem{ID: unit.ID, Title: unit.Title}
	setBreadcrumbs(data, []BreadcrumbItem{
		homeBreadcrumb(),
		{Name: "Courses", URL: "/courses"},
		{Name: courseBreadcrumbLabel(data["Course"].(courseListItem)), URL: "/courses/" + course.ID},
		{Name: unit.Title},
		{Name: "New Lesson", IsCurrent: true},
	})
	t.HTML(http.StatusOK, "lesson_new")
}

// CreateCourseUnit handles unit creation for a course.
func CreateCourseUnit(c flamego.Context, s session.Session) {
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

	unit, err := db.CreateCourseUnit(c.Request().Context(), db.CreateCourseUnitInput{
		CourseID:          c.Param("id"),
		Title:             c.Request().Form.Get("title"),
		Description:       c.Request().Form.Get("description"),
		InsertAfterItemID: c.Request().Form.Get("insert_after_item_id"),
		CreatedBy:         userID,
	})
	if err != nil {
		SetErrorFlash(s, humanizeCourseContentWriteError(err))
		redirectCoursePath(c, c.Param("id"))

		return
	}

	SetSuccessFlash(s, "Unit created")
	c.Redirect("/courses/"+unit.CourseID, http.StatusSeeOther)
}

// CreateUnitLesson handles lesson creation within a unit.
func CreateUnitLesson(c flamego.Context, s session.Session) {
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

	lesson, err := db.CreateUnitLesson(c.Request().Context(), db.CreateUnitLessonInput{
		CourseID:    c.Param("id"),
		UnitID:      c.Param("unitID"),
		Title:       c.Request().Form.Get("title"),
		Description: c.Request().Form.Get("description"),
		CreatedBy:   userID,
	})
	if err != nil {
		SetErrorFlash(s, humanizeCourseContentWriteError(err))
		redirectCoursePath(c, c.Param("id"))

		return
	}

	SetSuccessFlash(s, "Lesson created")
	c.Redirect("/courses/"+lesson.CourseID+"/lessons/"+lesson.ID, http.StatusSeeOther)
}

// UpdateUnitLesson handles lesson edits.
func UpdateUnitLesson(c flamego.Context, s session.Session) {
	if err := c.Request().ParseForm(); err != nil {
		SetErrorFlash(s, "Failed to parse form")
		redirectLessonPath(c, c.Param("id"), c.Param("lessonID"))

		return
	}

	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		redirectLessonPath(c, c.Param("id"), c.Param("lessonID"))

		return
	}

	lesson, err := db.UpdateUnitLesson(c.Request().Context(), db.UpdateUnitLessonInput{
		CourseID:    c.Param("id"),
		LessonID:    c.Param("lessonID"),
		Title:       c.Request().Form.Get("title"),
		Description: c.Request().Form.Get("description"),
		UpdatedBy:   userID,
	})
	if err != nil {
		SetErrorFlash(s, humanizeCourseContentWriteError(err))
		redirectLessonPath(c, c.Param("id"), c.Param("lessonID"))

		return
	}

	SetSuccessFlash(s, "Lesson updated")
	c.Redirect("/courses/"+lesson.CourseID+"/lessons/"+lesson.ID, http.StatusSeeOther)
}

// DeleteUnitLesson removes a lesson.
func DeleteUnitLesson(c flamego.Context, s session.Session) {
	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		redirectLessonPath(c, c.Param("id"), c.Param("lessonID"))

		return
	}

	if err := db.DeleteUnitLesson(c.Request().Context(), c.Param("id"), c.Param("lessonID"), userID); err != nil {
		SetErrorFlash(s, humanizeCourseContentWriteError(err))
		redirectLessonPath(c, c.Param("id"), c.Param("lessonID"))

		return
	}

	SetSuccessFlash(s, "Lesson deleted")
	redirectCoursePath(c, c.Param("id"))
}

// CreateLessonSlide handles slide creation within a lesson.
func CreateLessonSlide(c flamego.Context, s session.Session) {
	if err := c.Request().ParseForm(); err != nil {
		SetErrorFlash(s, "Failed to parse form")
		redirectLessonPath(c, c.Param("id"), c.Param("lessonID"))

		return
	}

	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		redirectLessonPath(c, c.Param("id"), c.Param("lessonID"))

		return
	}

	_, err := db.CreateLessonSlide(c.Request().Context(), db.CreateLessonSlideInput{
		CourseID:    c.Param("id"),
		LessonID:    c.Param("lessonID"),
		Title:       c.Request().Form.Get("title"),
		MarkdownRaw: c.Request().Form.Get("markdown_raw"),
		CreatedBy:   userID,
	})
	if err != nil {
		SetErrorFlash(s, humanizeCourseContentWriteError(err))
		redirectLessonPath(c, c.Param("id"), c.Param("lessonID"))

		return
	}

	SetSuccessFlash(s, "Slide added")
	redirectLessonPath(c, c.Param("id"), c.Param("lessonID"))
}

// UpdateLessonSlide handles slide editing within a lesson.
func UpdateLessonSlide(c flamego.Context, s session.Session) {
	if err := c.Request().ParseForm(); err != nil {
		SetErrorFlash(s, "Failed to parse form")
		redirectLessonPath(c, c.Param("id"), c.Param("lessonID"))

		return
	}

	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		redirectLessonPath(c, c.Param("id"), c.Param("lessonID"))

		return
	}

	_, err := db.UpdateLessonSlide(c.Request().Context(), db.UpdateLessonSlideInput{
		CourseID:    c.Param("id"),
		LessonID:    c.Param("lessonID"),
		SlideID:     c.Param("slideID"),
		Title:       c.Request().Form.Get("title"),
		MarkdownRaw: c.Request().Form.Get("markdown_raw"),
		UpdatedBy:   userID,
	})
	if err != nil {
		SetErrorFlash(s, humanizeCourseContentWriteError(err))
		redirectLessonPath(c, c.Param("id"), c.Param("lessonID"))

		return
	}

	SetSuccessFlash(s, "Slide updated")
	redirectLessonPath(c, c.Param("id"), c.Param("lessonID"))
}

// DeleteLessonSlide removes a slide from a lesson.
func DeleteLessonSlide(c flamego.Context, s session.Session) {
	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		redirectLessonPath(c, c.Param("id"), c.Param("lessonID"))

		return
	}

	err := db.DeleteLessonSlide(c.Request().Context(), db.DeleteLessonSlideInput{
		CourseID:  c.Param("id"),
		LessonID:  c.Param("lessonID"),
		SlideID:   c.Param("slideID"),
		DeletedBy: userID,
	})
	if err != nil {
		SetErrorFlash(s, humanizeCourseContentWriteError(err))
		redirectLessonPath(c, c.Param("id"), c.Param("lessonID"))

		return
	}

	SetSuccessFlash(s, "Slide deleted")
	redirectLessonPath(c, c.Param("id"), c.Param("lessonID"))
}

// MarkLessonComplete records completion for the current student's lesson.
func MarkLessonComplete(c flamego.Context, s session.Session) {
	userID, ok := getSessionUserID(s)
	if !ok {
		SetErrorFlash(s, "Unable to resolve current user")
		redirectLessonPath(c, c.Param("id"), c.Param("lessonID"))

		return
	}

	_, err := db.MarkLessonComplete(c.Request().Context(), db.MarkLessonCompleteInput{
		LessonID:  c.Param("lessonID"),
		StudentID: userID,
	})
	if err != nil {
		SetErrorFlash(s, humanizeCourseContentWriteError(err))
		redirectLessonPath(c, c.Param("id"), c.Param("lessonID"))

		return
	}

	SetSuccessFlash(s, "Lesson marked complete")
	redirectCoursePath(c, c.Param("id"))
}

// LessonDetail renders a lesson slide viewer and editor.
func LessonDetail(c flamego.Context, s session.Session, t template.Template, data template.Data) {
	setPage(data, "Lesson")
	data["IsCourses"] = true
	data["IncludeLessonViewerJS"] = true

	ctx := c.Request().Context()
	user, err := resolveSessionUser(ctx, s)
	if err != nil {
		SetErrorFlash(s, "Unable to resolve current user")
		c.Redirect("/courses", http.StatusSeeOther)

		return
	}

	lessonID := strings.TrimSpace(c.Param("lessonID"))
	if lessonID == "" {
		SetErrorFlash(s, "Missing lesson ID")
		redirectCoursePath(c, c.Param("id"))

		return
	}

	canView, err := db.CanUserViewLesson(ctx, user.ID.String(), lessonID)
	if err != nil {
		logger.Error("failed to check lesson visibility", "error", err)
		SetErrorFlash(s, "Failed to load lesson")
		redirectCoursePath(c, c.Param("id"))

		return
	}
	if !canView {
		SetErrorFlash(s, "Access restricted")
		redirectCoursePath(c, c.Param("id"))

		return
	}

	lesson, err := db.GetLessonByID(ctx, lessonID)
	if err != nil {
		logger.Error("failed to load lesson", "error", err)
		SetErrorFlash(s, humanizeCourseContentWriteError(err))
		redirectCoursePath(c, c.Param("id"))

		return
	}
	if lesson.CourseID != strings.TrimSpace(c.Param("id")) {
		SetErrorFlash(s, "Lesson not found")
		redirectCoursePath(c, c.Param("id"))

		return
	}

	course, err := db.GetCourseByID(ctx, lesson.CourseID)
	if err != nil {
		logger.Error("failed to load lesson course", "error", err)
		SetErrorFlash(s, "Failed to load lesson")
		redirectCoursePath(c, lesson.CourseID)

		return
	}

	canManage, err := db.CanUserManageLesson(ctx, user.ID.String(), lesson.ID)
	if err != nil {
		logger.Error("failed to check lesson management access", "error", err)
		SetErrorFlash(s, "Failed to load lesson")
		redirectCoursePath(c, lesson.CourseID)

		return
	}

	slides, err := db.ListLessonSlides(ctx, lesson.ID)
	if err != nil {
		logger.Error("failed to list lesson slides", "error", err)
		SetErrorFlash(s, "Failed to load lesson")
		redirectCoursePath(c, lesson.CourseID)

		return
	}

	data["Course"] = makeCourseListItem(*course)
	data["Unit"] = lessonUnitItem{ID: lesson.UnitID, Title: lesson.UnitTitle}
	data["Lesson"] = courseLessonItem{
		ID:          lesson.ID,
		UnitID:      lesson.UnitID,
		Title:       lesson.Title,
		Description: lesson.Description,
		Position:    lesson.Position,
		SlideCount:  lesson.SlideCount,
		UpdatedAt:   lesson.UpdatedAt.Format("Jan 2, 2006 15:04"),
	}
	setBreadcrumbs(data, []BreadcrumbItem{
		homeBreadcrumb(),
		{Name: "Courses", URL: "/courses"},
		{Name: courseBreadcrumbLabel(data["Course"].(courseListItem)), URL: "/courses/" + course.ID},
		{Name: data["Lesson"].(courseLessonItem).Title, IsCurrent: true},
	})
	data["CanManageLesson"] = canManage
	data["LessonSlides"] = makeLessonSlideViews(slides)
	data["LessonSlideCount"] = len(slides)
	data["CanMarkLessonComplete"] = false
	data["LessonCompleted"] = false
	if user.Role.IsStudent() {
		completed, err := db.HasStudentCompletedLesson(ctx, lesson.ID, user.ID.String())
		if err != nil {
			logger.Error("failed to check lesson completion", "error", err)
			SetErrorFlash(s, "Failed to load lesson")
			redirectCoursePath(c, lesson.CourseID)

			return
		}

		data["CanMarkLessonComplete"] = len(slides) > 0
		data["LessonCompleted"] = completed
	}

	t.HTML(http.StatusOK, "lesson")
}

func loadCourseOutlineItems(ctx context.Context, courseID string) ([]courseOutlineItemView, error) {
	return loadCourseOutlineItemsForStudent(ctx, courseID, "")
}

func loadCourseOutlineItemsForStudent(ctx context.Context, courseID, studentID string) ([]courseOutlineItemView, error) {
	outline, err := db.ListCourseOutline(ctx, courseID)
	if err != nil {
		return nil, err
	}

	completedSet := map[string]bool{}
	if studentID != "" {
		ids, err := db.ListStudentCompletedLessonIDs(ctx, courseID, studentID)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			completedSet[id] = true
		}
	}

	items := make([]courseOutlineItemView, 0, len(outline))
	for _, item := range outline {
		viewItem := courseOutlineItemView{
			ID:       item.ID,
			Position: item.Position,
			ItemType: string(item.ItemType),
		}

		if item.Unit != nil {
			lessons, err := db.ListUnitLessons(ctx, item.Unit.ID)
			if err != nil {
				return nil, err
			}

			viewItem.Unit = &courseUnitContentItem{
				ID:          item.Unit.ID,
				Title:       item.Unit.Title,
				Description: item.Unit.Description,
				LessonCount: len(lessons),
				Lessons:     makeCourseLessonItems(lessons, completedSet),
			}
		}
		if item.Assignment != nil {
			assignmentItem := makeAssignmentListItem(*item.Assignment)
			viewItem.Assignment = &assignmentItem
		}

		items = append(items, viewItem)
	}

	return items, nil
}

func buildOutlineInsertOptions(items []courseOutlineItemView) []outlineInsertOptionItem {
	options := make([]outlineInsertOptionItem, 0, len(items))
	for _, item := range items {
		switch {
		case item.Unit != nil:
			options = append(options, outlineInsertOptionItem{
				ID:    item.ID,
				Label: "After unit: " + item.Unit.Title,
			})
		case item.Assignment != nil:
			options = append(options, outlineInsertOptionItem{
				ID:    item.ID,
				Label: "After assignment: " + item.Assignment.Title,
			})
		}
	}

	return options
}

func makeCourseLessonItems(values []db.UnitLesson, completedSet map[string]bool) []courseLessonItem {
	items := make([]courseLessonItem, 0, len(values))
	for _, value := range values {
		items = append(items, courseLessonItem{
			ID:          value.ID,
			UnitID:      value.UnitID,
			Title:       value.Title,
			Description: value.Description,
			Position:    value.Position,
			SlideCount:  value.SlideCount,
			UpdatedAt:   value.UpdatedAt.Format("Jan 2, 2006 15:04"),
			Completed:   completedSet[value.ID],
		})
	}

	return items
}

func makeLessonSlideViews(values []db.LessonSlide) []lessonSlideView {
	items := make([]lessonSlideView, 0, len(values))
	for _, value := range values {
		renderedHTML, err := renderLessonMarkdown(value.MarkdownRaw)
		if err != nil {
			renderedHTML = htmltemplate.HTML("<p>Unable to render slide.</p>")
		}

		items = append(items, lessonSlideView{
			ID:           value.ID,
			Title:        value.Title,
			MarkdownRaw:  value.MarkdownRaw,
			RenderedHTML: renderedHTML,
			Position:     value.Position,
			UpdatedAt:    value.UpdatedAt.Format("Jan 2, 2006 15:04"),
		})
	}

	return items
}

func renderLessonMarkdown(raw string) (htmltemplate.HTML, error) {
	var rendered bytes.Buffer
	if err := lessonMarkdownRenderer.Convert([]byte(strings.TrimSpace(raw)), &rendered); err != nil {
		return "", err
	}

	return htmltemplate.HTML(rendered.String()), nil
}

func redirectLessonPath(c flamego.Context, courseID string, lessonID string) {
	if strings.TrimSpace(courseID) == "" || strings.TrimSpace(lessonID) == "" {
		redirectCoursePath(c, courseID)

		return
	}

	c.Redirect("/courses/"+courseID+"/lessons/"+lessonID, http.StatusSeeOther)
}

func humanizeCourseContentWriteError(err error) string {
	switch {
	case err == nil:
		return ""
	case err == db.ErrCourseUnitTitleRequired:
		return err.Error()
	case err == db.ErrCourseUnitNotFound:
		return err.Error()
	case err == db.ErrUnitLessonTitleRequired:
		return err.Error()
	case err == db.ErrUnitLessonNotFound:
		return err.Error()
	case err == db.ErrLessonSlideMarkdownRequired:
		return err.Error()
	case err == db.ErrLessonSlideNotFound:
		return err.Error()
	case err == db.ErrStudentRequired:
		return "Only students can mark lessons complete"
	case err == db.ErrOutlineItemNotFound:
		return "Choose a valid insertion point"
	case err == db.ErrTeacherRequired:
		return "Teacher access required"
	case err == db.ErrAccessDenied:
		return "You can only manage content for courses you teach"
	case err == db.ErrCourseNotFound:
		return err.Error()
	default:
		return "Failed to save course content"
	}
}
