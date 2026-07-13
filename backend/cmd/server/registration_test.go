package main

import (
	"path/filepath"
	"testing"
)

func TestCanonicalRosterName(t *testing.T) {
	name, minor := canonicalRosterName(" 高亚拉（辅修） ")
	if name != "高亚拉" || !minor {
		t.Fatalf("unexpected canonical result: %q, %v", name, minor)
	}
	if got := usernameFromName("张迦勒"); got != "zhangjiale" {
		t.Fatalf("unexpected pinyin username: %s", got)
	}
}

func TestParseProductionRoster(t *testing.T) {
	path := filepath.Join("..", "..", "..", "2026年度门训生命季_分组及组长.xlsx")
	rows, err := parseRoster(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 117 {
		t.Fatalf("expected 117 roster entries, got %d", len(rows))
	}
	leaders := 0
	groups := map[string]bool{}
	for _, row := range rows {
		groups[row.GroupCode] = true
		if row.IsLeader {
			leaders++
		}
	}
	if len(groups) != 8 || leaders != 11 {
		t.Fatalf("expected 8 groups and 11 leaders, got %d and %d", len(groups), leaders)
	}
}
