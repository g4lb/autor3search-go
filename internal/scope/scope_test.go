package scope

import "testing"

func TestMatch(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		rel      string
		want     bool
	}{
		{"root wildcard matches all", []string{"./..."}, "internal/a/b.go", true},
		{"subtree wildcard matches", []string{"./internal/..."}, "internal/a/b.go", true},
		{"subtree wildcard rejects sibling", []string{"./internal/..."}, "cmd/main.go", false},
		{"exact dir matches direct child", []string{"./pkg"}, "pkg/a.go", true},
		{"exact dir rejects nested", []string{"./pkg"}, "pkg/sub/a.go", false},
		{"multiple patterns", []string{"./cmd/...", "./internal/..."}, "cmd/x/main.go", true},
		{"no patterns matches nothing", nil, "a.go", false},
		{"prefix must be a path boundary", []string{"./internal/..."}, "internalfoo/a.go", false},
		{"patterns without leading dot slash", []string{"internal/..."}, "internal/a.go", true},
		{"empty pattern matches nothing (a.go)", []string{""}, "a.go", false},
		{"empty pattern matches nothing (go.mod)", []string{""}, "go.mod", false},

		// A path that escapes the root is out of scope under every pattern,
		// including — especially — the one that means "the whole repository".
		// path.Clean keeps the leading "..", and "./..." compiles to a
		// recursive rule with an empty prefix that otherwise matches anything,
		// so the widest legitimate pattern was also the widest illegitimate one.
		{"root wildcard rejects parent escape", []string{"./..."}, "../evil.go", false},
		{"root wildcard rejects deep escape", []string{"./..."}, "../../etc/passwd", false},
		{"root wildcard rejects bare parent", []string{"./..."}, "..", false},
		{"root wildcard rejects absolute path", []string{"./..."}, "/etc/passwd", false},
		{"subtree wildcard rejects escape", []string{"./internal/..."}, "../evil.go", false},
		{"escape through a subdirectory is rejected", []string{"./..."}, "internal/../../evil.go", false},
		{"climbing back inside is still in scope", []string{"./..."}, "internal/../cmd/main.go", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := New(tt.patterns).Match(tt.rel); got != tt.want {
				t.Errorf("Match(%q) = %v, want %v", tt.rel, got, tt.want)
			}
		})
	}
}
