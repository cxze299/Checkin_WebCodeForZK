package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateStudyWeekImport(t *testing.T) {
	valid := []studyWeekInput{
		{
			StartDate: "2026-07-08",
			EndDate:   "2026-07-14",
			Title:     "Week 2",
		},
		{
			StartDate: "2026-07-01",
			EndDate:   "2026-07-07",
			Title:     "Week 1",
			Readings:  []weekTaskBinding{{URL: "/Book/week-1.pdf"}},
		},
	}
	if err := validateStudyWeekImport(valid); err != nil {
		t.Fatalf("valid non-overlapping import rejected: %v", err)
	}

	tests := []struct {
		name  string
		weeks []studyWeekInput
		want  string
	}{
		{
			name:  "invalid week is validated",
			weeks: []studyWeekInput{{StartDate: "2026-07-01", EndDate: "2026-07-07", Title: " "}},
			want:  "week_title_required",
		},
		{
			name: "invalid nested resource is validated",
			weeks: []studyWeekInput{{
				StartDate: "2026-07-01",
				EndDate:   "2026-07-07",
				Title:     "Week 1",
				Videos:    []weekTaskBinding{{URL: "//evil.example/video.mp4"}},
			}},
			want: "invalid_resource_url",
		},
		{
			name: "overlapping ranges are rejected",
			weeks: []studyWeekInput{
				{StartDate: "2026-07-01", EndDate: "2026-07-07", Title: "Week 1"},
				{StartDate: "2026-07-07", EndDate: "2026-07-13", Title: "Week 2"},
			},
			want: "week_overlap",
		},
		{
			name: "duplicate ranges are rejected",
			weeks: []studyWeekInput{
				{StartDate: "2026-07-01", EndDate: "2026-07-07", Title: "First"},
				{StartDate: "2026-07-01", EndDate: "2026-07-07", Title: "Duplicate"},
			},
			want: "week_overlap",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateStudyWeekImport(tt.weeks)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("validateStudyWeekImport() error = %v, want %q", err, tt.want)
			}
		})
	}
}

type repeatedByteReader struct{}

func (repeatedByteReader) Read(p []byte) (int, error) {
	for index := range p {
		p[index] = 'x'
	}
	return len(p), nil
}

func TestAdminImportStudyWeeksExcelRejectsOversizedBody(t *testing.T) {
	prefix := strings.NewReader("--agp-boundary\r\n" +
		"Content-Disposition: form-data; name=\"file\"; filename=\"weeks.xlsx\"\r\n" +
		"Content-Type: application/vnd.openxmlformats-officedocument.spreadsheetml.sheet\r\n\r\n")
	body := io.MultiReader(prefix, io.LimitReader(repeatedByteReader{}, maxStudyWeeksImportBytes+1))
	req := httptest.NewRequest(http.MethodPost, "/api/admin/imports/study-weeks", body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=agp-boundary")
	req = req.WithContext(context.WithValue(req.Context(), currentUserKey, currentUser{
		ID:             1,
		CurrentGroupID: 1,
	}))
	recorder := httptest.NewRecorder()

	(&app{}).handleAdminImportStudyWeeksExcel(recorder, req)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusRequestEntityTooLarge, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "study_weeks_import_too_large") {
		t.Fatalf("unexpected response body: %s", recorder.Body.String())
	}
}
