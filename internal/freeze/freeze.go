// Package freeze snapshots test files at baseline time and restores them
// before every evaluation, so an agent cannot weaken its own success criteria.
package freeze

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Locations relative to the run's out-of-tree state.StateDir — never to the
// repository root. The frozen store and its manifest are part of what the
// success metric depends on, so they must live where the agent being
// measured cannot reach them; a caller that joins these onto the repository
// root instead would silently reintroduce that hole.
const (
	StoreDir     = "frozen"
	ManifestPath = "frozen/manifest.json"
)

// Manifest maps repo-relative file paths to their sha256 at baseline time.
type Manifest struct {
	Files map[string]string `json:"files"`
}

// ErrSymlink indicates a symlink somewhere along a frozen test file's path.
// Both Snapshot and Restore use os.WriteFile / os.ReadFile under the hood,
// which follow symlinks: writing to a symlinked destination writes through it
// to wherever it points, potentially outside the repository entirely. Rather
// than follow the link, Snapshot and Restore refuse and return an error
// wrapping ErrSymlink, so callers can distinguish "tampering detected" from
// an ordinary I/O failure.
var ErrSymlink = errors.New("frozen test file path is a symlink")

// ErrStoreTampered indicates a frozen golden copy no longer hashes to what
// the manifest recorded for it at baseline time.
//
// The manifest hash is the whole point of recording one: without checking it,
// the store is trusted blindly, and an agent that rewrites a file under
// <StateDir>/frozen has its weakened test restored into the working tree by
// every subsequent eval — the exact outcome freezing exists to prevent.
// Verifying it means tampering has to alter the store AND the manifest
// consistently to go unnoticed, instead of just the store.
var ErrStoreTampered = errors.New("frozen store copy does not match the hash recorded at baseline")

// lstatIsSymlink reports whether path exists and is a symlink, using Lstat
// (not Stat) so the check is about the path itself, not whatever it points
// to. Stat would follow the link and report the target's mode, silently
// defeating the check this exists to make. A path that does not exist is
// not a symlink and is left to the caller's own not-exist handling.
func lstatIsSymlink(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.Mode()&os.ModeSymlink != 0, nil
}

// symlinkComponent returns the first component of rel, beneath root, that is
// a symlink — as a slash-separated path relative to root — or "" when none
// is. It is the check every read and write of a frozen file goes through.
//
// Checking only the FINAL component is not enough, which is the hole this
// closes: os.ReadFile and os.WriteFile resolve the whole path, so replacing a
// parent DIRECTORY with a link ("pkg" swapped for a link to /elsewhere)
// redirects the write exactly as effectively as replacing the file itself
// does, and lands the frozen content outside the repository. Lstat on the
// file then reports a perfectly ordinary regular file, because it has already
// followed the link to get there.
//
// root itself is deliberately not examined. A repository legitimately reached
// through a symlinked ancestor — macOS's /tmp, a home directory on a linked
// volume, a checkout under a symlinked mount — is not tampering, and refusing
// to work there would break ordinary setups.
func symlinkComponent(root, rel string) (string, error) {
	// path.Clean for the same reason as in safeJoin: filepath.Clean would
	// hand back a backslash-separated path on Windows, the Split on "/"
	// would yield the whole path as a single element, and every parent
	// directory would go unchecked — which is exactly the hole this
	// function exists to close.
	parts := strings.Split(path.Clean(filepath.ToSlash(rel)), "/")
	path := root
	for i, part := range parts {
		path = filepath.Join(path, part)
		isLink, err := lstatIsSymlink(path)
		if err != nil {
			return "", err
		}
		if isLink {
			return strings.Join(parts[:i+1], "/"), nil
		}
	}
	return "", nil
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// safeJoin joins rel onto root, rejecting anything that would escape it.
// Manifest entries come from a JSON file on disk, so they are untrusted
// input: Restore writes through them before every evaluation.
func safeJoin(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("frozen path %q must be relative", rel)
	}
	// path.Clean, not filepath.Clean, and this is the whole point of the
	// ToSlash above: on Windows filepath.Clean converts the separators
	// straight back to backslashes, so "../escape_test.go" cleans to
	// `..\escape_test.go`, the "../" test below does not match, and the
	// guard admits the very path it exists to reject. path.Clean is
	// slash-only on every platform, so the check means the same thing
	// everywhere.
	clean := path.Clean(filepath.ToSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("frozen path %q escapes the repository root", rel)
	}
	return filepath.Join(root, filepath.FromSlash(clean)), nil
}

// Snapshot copies each file into storeDir and records its hash.
// Paths are repo-relative and are preserved inside the store.
func Snapshot(repoRoot, storeDir string, files []string) (*Manifest, error) {
	m := &Manifest{Files: map[string]string{}}
	for _, rel := range files {
		src, err := safeJoin(repoRoot, rel)
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", rel, err)
		}
		if link, err := symlinkComponent(repoRoot, rel); err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", rel, err)
		} else if link != "" {
			return nil, fmt.Errorf("snapshot %s: %w: %s is a symlink, and symlinked test files "+
				"are unsupported because the harness cannot guarantee restoring them stays inside "+
				"the repository", rel, ErrSymlink, link)
		}
		b, err := os.ReadFile(src)
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", rel, err)
		}
		dst, err := safeJoin(storeDir, rel)
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", rel, err)
		}
		// The store is harness-owned, but a directory left behind by an
		// earlier attempt under the same tag is not necessarily pristine:
		// refuse to write the golden copy through a link there either.
		if link, err := symlinkComponent(storeDir, rel); err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", rel, err)
		} else if link != "" {
			return nil, fmt.Errorf("snapshot %s: %w: %s is a symlink inside the frozen store",
				rel, ErrSymlink, link)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", rel, err)
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", rel, err)
		}
		m.Files[rel] = hashBytes(b)
	}
	return m, nil
}

// Restore rewrites every frozen file in the working tree from the store,
// recreating files the agent deleted. It returns the paths it changed.
//
// Both sides are checked against the hash the manifest recorded at baseline,
// and the working tree is examined BEFORE the store is read. That ordering is
// what makes the common case — an eval where the agent touched no test file —
// cost one read per frozen file instead of two: the destination already
// hashes to the manifest value, so the golden copy is never opened at all.
// The store is read, and validated against the same hash, only for a file
// that actually has to be rewritten.
func Restore(repoRoot, storeDir string, m *Manifest) ([]string, error) {
	var changed []string
	for _, rel := range m.sortedPaths() {
		dst, err := safeJoin(repoRoot, rel)
		if err != nil {
			return nil, fmt.Errorf("restore %s: %w", rel, err)
		}
		// Before any read or write of dst: os.ReadFile follows links just as
		// os.WriteFile does, so this has to come first to avoid reading
		// through one and concluding the file is fine.
		if link, err := symlinkComponent(repoRoot, rel); err != nil {
			return nil, fmt.Errorf("restore %s: %w", rel, err)
		} else if link != "" {
			return nil, fmt.Errorf("restore %s: %w: %s was replaced by a symlink; refusing to "+
				"write through it, which could reach a file outside the repository",
				rel, ErrSymlink, link)
		}
		if got, err := os.ReadFile(dst); err == nil && hashBytes(got) == m.Files[rel] {
			continue // already the frozen content; the store need not be read
		}

		src, err := safeJoin(storeDir, rel)
		if err != nil {
			return nil, fmt.Errorf("restore %s: %w", rel, err)
		}
		if link, err := symlinkComponent(storeDir, rel); err != nil {
			return nil, fmt.Errorf("restore %s: %w", rel, err)
		} else if link != "" {
			return nil, fmt.Errorf("restore %s: %w: %s is a symlink inside the frozen store; "+
				"refusing to restore content read through it", rel, ErrSymlink, link)
		}
		want, err := os.ReadFile(src)
		if err != nil {
			return nil, fmt.Errorf("restore %s: %w", rel, err)
		}
		if got := hashBytes(want); got != m.Files[rel] {
			return nil, fmt.Errorf("restore %s: %w: store copy hashes to %s, manifest records %s",
				rel, ErrStoreTampered, got, m.Files[rel])
		}

		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, fmt.Errorf("restore %s: %w", rel, err)
		}
		if err := os.WriteFile(dst, want, 0o644); err != nil {
			return nil, fmt.Errorf("restore %s: %w", rel, err)
		}
		changed = append(changed, rel)
	}
	return changed, nil
}

// Verify reports which frozen files currently differ from the baseline.
// A deleted file counts as changed.
func Verify(repoRoot string, m *Manifest) ([]string, error) {
	var changed []string
	for _, rel := range m.sortedPaths() {
		path, err := safeJoin(repoRoot, rel)
		if err != nil {
			return nil, fmt.Errorf("verify %s: %w", rel, err)
		}
		if link, err := symlinkComponent(repoRoot, rel); err != nil {
			return nil, fmt.Errorf("verify %s: %w", rel, err)
		} else if link != "" {
			// A frozen path with a symlink anywhere along it is at least as
			// suspicious as a deleted one — report it as changed rather than
			// following the link to read whatever it points at.
			changed = append(changed, rel)
			continue
		}
		b, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			changed = append(changed, rel)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("verify %s: %w", rel, err)
		}
		if hashBytes(b) != m.Files[rel] {
			changed = append(changed, rel)
		}
	}
	return changed, nil
}

func (m *Manifest) sortedPaths() []string {
	out := make([]string, 0, len(m.Files))
	for p := range m.Files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Save writes the manifest as indented JSON.
func (m *Manifest) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// LoadManifest reads a manifest written by Save.
func LoadManifest(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Files == nil {
		m.Files = map[string]string{}
	}
	return &m, nil
}
