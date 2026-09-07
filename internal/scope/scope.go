// Package scope decides which files an agent is allowed to modify.
package scope

import (
	"path"
	"path/filepath"
	"strings"
)

type rule struct {
	prefix    string // normalized directory prefix, "" means repository root
	recursive bool
}

// Matcher tests repo-relative paths against a set of go-style patterns.
type Matcher struct {
	rules []rule
}

// New compiles patterns such as "./...", "./internal/..." or "./pkg". An
// empty or whitespace-only pattern is skipped rather than treated as the
// repository root, so a blank entry (a stray empty item in a config list,
// say) matches nothing instead of silently granting root-level access.
func New(patterns []string) *Matcher {
	m := &Matcher{}
	for _, raw := range patterns {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		p := path.Clean(strings.TrimPrefix(trimmed, "./"))
		recursive := false
		if p == "..." {
			p, recursive = "", true
		} else if strings.HasSuffix(p, "/...") {
			p, recursive = strings.TrimSuffix(p, "/..."), true
		}
		if p == "." {
			p = ""
		}
		m.rules = append(m.rules, rule{prefix: p, recursive: recursive})
	}
	return m
}

// Match reports whether rel is inside the allowed scope.
//
// rel must be a path relative to the repository root. One that is absolute,
// or that climbs out of the root, is never in scope no matter what the
// patterns say — and that has to be rejected explicitly, because it would
// otherwise be ADMITTED: path.Clean leaves the leading ".." in place, and the
// default "./..." pattern compiles to a recursive rule with an empty prefix,
// which matches every path handed to it. So the one pattern that says "the
// whole repository" would have been the one saying "anywhere on the disk".
//
// Nothing produces such a path today — callers pass the output of git
// diff --name-only and git ls-files, which are always root-relative — so this
// is the gate refusing to depend on that staying true.
func (m *Matcher) Match(rel string) bool {
	if path.IsAbs(rel) || filepath.IsAbs(rel) {
		return false
	}
	rel = path.Clean(strings.TrimPrefix(rel, "./"))
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return false
	}
	for _, r := range m.rules {
		if r.recursive {
			if r.prefix == "" || rel == r.prefix || strings.HasPrefix(rel, r.prefix+"/") {
				return true
			}
			continue
		}
		if path.Dir(rel) == r.prefix || (r.prefix == "" && path.Dir(rel) == ".") {
			return true
		}
	}
	return false
}
