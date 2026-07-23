package main

import "testing"

func TestResourceLibraryKey(t *testing.T) {
	tests := []struct {
		name string
		item map[string]any
		want string
	}{
		{"uploaded asset", map[string]any{"id": uint64(42), "url": "/api/assets/42/download"}, "asset:42"},
		{"static file", map[string]any{"category": "book", "relative_path": "课程/第一课.pdf"}, "static:book:课程/第一课.pdf"},
		{"external url", map[string]any{"category": "video", "url": "https://example.test/video.mp4"}, "url:https://example.test/video.mp4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resourceLibraryKey(tt.item); got != tt.want {
				t.Fatalf("resourceLibraryKey() = %q, want %q", got, tt.want)
			}
		})
	}
}
