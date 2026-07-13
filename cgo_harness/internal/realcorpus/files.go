package realcorpus

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// File describes a regular file accepted by the real-corpus traversal policy.
type File struct {
	Path string
	Size int64
}

// CleanSubdir validates and normalizes a lock-file subdirectory.
func CleanSubdir(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "." {
		return "", true
	}
	clean := filepath.Clean(filepath.FromSlash(raw))
	if clean == "." || clean == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	return clean, true
}

// WalkMatchingFiles walks root using the benchmark's directory and file policy.
// vendor and dist are intentionally eligible because generated and vendored
// sources are valid performance witnesses.
func WalkMatchingFiles(root string, matches func(relativePath string) bool, visit func(File) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".gradle", "bazel-bin", "bazel-out", "bazel-testlogs", "build", "node_modules", "target":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		relPath := path
		if rel, err := filepath.Rel(root, path); err == nil {
			relPath = rel
		}
		if !matches(relPath) {
			return nil
		}
		return visit(File{Path: path, Size: info.Size()})
	})
}
