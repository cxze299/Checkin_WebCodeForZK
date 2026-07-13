package main

import "testing"

func TestCleanResourceFolderPath(t *testing.T) {
	for _, value := range []string{"../etc", "a/../../etc", "/../../etc"} {
		if _, err := cleanResourceFolderPath(value, false); err == nil {
			t.Fatalf("expected unsafe path %q to fail", value)
		}
	}
	if got, err := cleanResourceFolderPath("课程/第一周", false); err != nil || got == "" {
		t.Fatalf("expected valid nested path, got %q, %v", got, err)
	}
}

func TestCleanResourceFolderName(t *testing.T) {
	for _, value := range []string{"", "..", "a/b", `a\b`} {
		if _, err := cleanResourceFolderName(value); err == nil {
			t.Fatalf("expected unsafe name %q to fail", value)
		}
	}
	if got, err := cleanResourceFolderName("第一周资料"); err != nil || got != "第一周资料" {
		t.Fatalf("unexpected valid name result: %q, %v", got, err)
	}
}
