package main

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestTokenClaimsAuthVersionAndExpiry(t *testing.T) {
	a := &app{secret: []byte("0123456789abcdef0123456789abcdef")}
	before := time.Now()
	claims := newTokenClaims(42, 7, 3)
	if claims.UserID != 42 || claims.CurrentGroupID != 7 || claims.AuthVersion != 3 {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	if expires := time.Unix(claims.ExpiresAt, 0); expires.Before(before.Add(tokenTTL-time.Second)) || expires.After(time.Now().Add(tokenTTL+time.Second)) {
		t.Fatalf("unexpected token expiry: %s", expires)
	}

	token, err := a.signToken(claims)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := a.verifyToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if verified.AuthVersion != claims.AuthVersion || verified.UserID != claims.UserID {
		t.Fatalf("verified claims changed: %+v", verified)
	}

	for name, invalid := range map[string]tokenClaims{
		"expired": {
			UserID: 1, AuthVersion: 1, ExpiresAt: time.Now().Add(-time.Second).Unix(),
		},
		"missing auth version": {
			UserID: 1, AuthVersion: 0, ExpiresAt: time.Now().Add(time.Minute).Unix(),
		},
		"missing user": {
			UserID: 0, AuthVersion: 1, ExpiresAt: time.Now().Add(time.Minute).Unix(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			token, err := a.signToken(invalid)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.verifyToken(token); err == nil {
				t.Fatal("expected invalid token to be rejected")
			}
		})
	}
}

func TestValidResourceURL(t *testing.T) {
	tests := map[string]bool{
		"":                              true,
		"/Book/lesson.pdf":              true,
		"https://example.com/video.mp4": true,
		"http://example.com/file.pdf":   true,
		"//evil.example/file.pdf":       false,
		"///evil.example/file.pdf":      false,
		"\\\\evil.example\\file.pdf":    false,
		"/Book\\evil.example\\file.pdf": false,
		"javascript:alert(1)":           false,
		"https:///missing-host.pdf":     false,
	}
	for value, want := range tests {
		if got := validResourceURL(value); got != want {
			t.Errorf("validResourceURL(%q)=%v, want %v", value, got, want)
		}
	}
}

func TestValidateStudyWeekInput(t *testing.T) {
	valid := studyWeekInput{
		StartDate: "2026-07-13",
		EndDate:   "2026-07-19",
		Title:     "第一周",
		Readings:  []weekTaskBinding{{Title: "读物", URL: "/Book/week-1.pdf"}},
		Videos:    []weekTaskBinding{{Title: "视频", URL: "https://example.com/week-1.mp4"}},
	}
	if err := validateStudyWeekInput(valid); err != nil {
		t.Fatalf("valid week rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*studyWeekInput)
	}{
		{"invalid start", func(v *studyWeekInput) { v.StartDate = "2026/07/13" }},
		{"end before start", func(v *studyWeekInput) { v.EndDate = "2026-07-12" }},
		{"missing title", func(v *studyWeekInput) { v.Title = " " }},
		{"protocol relative resource", func(v *studyWeekInput) { v.Readings[0].URL = "//evil.example/week-1.pdf" }},
		{"unsupported resource scheme", func(v *studyWeekInput) { v.Videos[0].URL = "javascript:alert(1)" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := valid
			candidate.Readings = append([]weekTaskBinding(nil), valid.Readings...)
			candidate.Videos = append([]weekTaskBinding(nil), valid.Videos...)
			tt.mutate(&candidate)
			if err := validateStudyWeekInput(candidate); err == nil {
				t.Fatal("expected invalid week to be rejected")
			}
		})
	}
}

func TestUploadCategoryExtensionAndContentType(t *testing.T) {
	for _, value := range []string{"", "book", "MARKDOWN", "handout", "outline", "video", "mentor", "uploaded"} {
		if _, err := normalizeAssetCategory(value); err != nil {
			t.Errorf("valid category %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{"../book", "book/child", "unknown", " book.txt "} {
		if _, err := normalizeAssetCategory(value); err == nil {
			t.Errorf("invalid category %q accepted", value)
		}
	}

	for _, ext := range []string{".pdf", ".PDF", ".md", ".png", ".jpeg", ".webp", ".mp4", ".mov"} {
		if !allowedUploadExtension(ext) {
			t.Errorf("valid extension %q rejected", ext)
		}
	}
	for _, ext := range []string{"", ".html", ".svg", ".exe", ".php"} {
		if allowedUploadExtension(ext) {
			t.Errorf("unsafe extension %q accepted", ext)
		}
	}

	if !allowedUploadContentType(".pdf", "application/pdf") || allowedUploadContentType(".pdf", "text/html") {
		t.Fatal("PDF content type validation is incorrect")
	}
	if !allowedUploadContentType(".jpg", "image/jpeg") || allowedUploadContentType(".jpg", "image/png") {
		t.Fatal("JPEG content type validation is incorrect")
	}
}

func TestValidCheckinTaskType(t *testing.T) {
	for _, taskType := range []string{"daily_devotion", "weekly_book", "weekly_video", "weekly_verse"} {
		if !validCheckinTaskType(taskType) {
			t.Errorf("valid task type %q rejected", taskType)
		}
	}
	for _, taskType := range []string{"", "weekly_outline", "reflection", "recite_exam", "admin", "weekly_book "} {
		if validCheckinTaskType(taskType) {
			t.Errorf("invalid task type %q accepted", taskType)
		}
	}
}

func TestValidateLocalBackupPayload(t *testing.T) {
	valid := localBackupPayload{
		Version:  1,
		Settings: map[string]any{"site_name": "AGP"},
		Members:  []localBackupMember{{Username: "member1", DisplayName: "成员一"}},
		Weeks: []studyWeekInput{{
			StartDate: "2026-07-13", EndDate: "2026-07-19", Title: "第一周",
		}},
		Checkins: []localBackupCheckin{{Username: "member1", LogicalDate: "2026-07-13", TaskType: "reflection"}},
	}
	if err := validateLocalBackupPayload(valid); err != nil {
		t.Fatalf("valid backup rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*localBackupPayload)
	}{
		{"unsupported version", func(v *localBackupPayload) { v.Version = 2 }},
		{"duplicate member", func(v *localBackupPayload) { v.Members = append(v.Members, v.Members[0]) }},
		{"invalid checkin date", func(v *localBackupPayload) { v.Checkins[0].LogicalDate = "2026/07/13" }},
		{"invalid checkin type", func(v *localBackupPayload) { v.Checkins[0].TaskType = "administrator" }},
		{"overlapping week", func(v *localBackupPayload) {
			v.Weeks = append(v.Weeks, studyWeekInput{StartDate: "2026-07-18", EndDate: "2026-07-25", Title: "第二周"})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := valid
			candidate.Members = append([]localBackupMember(nil), valid.Members...)
			candidate.Weeks = append([]studyWeekInput(nil), valid.Weeks...)
			candidate.Checkins = append([]localBackupCheckin(nil), valid.Checkins...)
			tt.mutate(&candidate)
			if err := validateLocalBackupPayload(candidate); err == nil {
				t.Fatal("expected invalid backup to be rejected")
			}
		})
	}
}

func TestLoginLimiterPrunesIdleAndOldestEntries(t *testing.T) {
	l := newLoginLimiter()
	now := time.Now()
	l.failures["stale"] = loginFailure{Count: 1, LastSeen: now.Add(-loginFailureTTL - time.Minute)}
	for i := 0; i < loginFailureCap+2; i++ {
		l.failures[fmt.Sprintf("recent-%d", i)] = loginFailure{Count: 1, LastSeen: now.Add(time.Duration(i) * time.Nanosecond)}
	}
	l.mu.Lock()
	l.pruneLocked(now)
	l.mu.Unlock()
	if _, ok := l.failures["stale"]; ok {
		t.Fatal("stale failure was not pruned")
	}
	if len(l.failures) != loginFailureCap {
		t.Fatalf("limiter retained %d entries, want %d", len(l.failures), loginFailureCap)
	}
	if _, ok := l.failures["recent-0"]; ok {
		t.Fatal("oldest recent failure should be evicted at capacity")
	}
}

func TestLoginLimiterConcurrentAccess(t *testing.T) {
	l := newLoginLimiter()
	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for attempt := 0; attempt < 50; attempt++ {
				ip := fmt.Sprintf("192.0.2.%d", worker)
				username := fmt.Sprintf("user-%d", attempt%5)
				l.fail(ip, username)
				_ = l.blocked(ip, username)
				if attempt%11 == 0 {
					l.success(ip, username)
				}
			}
		}(worker)
	}
	wg.Wait()
}

func TestValidateAvatarDimensions(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {512, 512}, {6000, 4000}} {
		if err := validateAvatarDimensions(size[0], size[1]); err != nil {
			t.Errorf("valid dimensions %dx%d rejected: %v", size[0], size[1], err)
		}
	}
	for _, size := range [][2]int{{0, 512}, {-1, 512}, {maxAvatarSide + 1, 1}, {8000, 4000}} {
		if err := validateAvatarDimensions(size[0], size[1]); err == nil {
			t.Errorf("invalid dimensions %dx%d accepted", size[0], size[1])
		}
	}
}
