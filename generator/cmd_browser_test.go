package main

import (
	"path/filepath"
	"testing"
)

func TestAbsCSVList(t *testing.T) {
	got, err := absCSVList("  a , , b/c ,")
	if err != nil {
		t.Fatalf("absCSVList: %v", err)
	}
	// Empty entries are dropped; the rest are abs-resolved against CWD.
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d: %v", len(got), got)
	}
	for _, p := range got {
		if !filepath.IsAbs(p) {
			t.Errorf("entry not absolute: %q", p)
		}
	}
}

func TestAppendUnique(t *testing.T) {
	list := appendUnique(nil, "/x")
	list = appendUnique(list, "/y")
	list = appendUnique(list, "/x") // dup, ignored
	if len(list) != 2 {
		t.Fatalf("want 2 unique entries, got %d: %v", len(list), list)
	}
	if list[0] != "/x" || list[1] != "/y" {
		t.Errorf("unexpected order/content: %v", list)
	}
}
