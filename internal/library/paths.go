// Package library holds shared helpers for working with track file paths.
//
// Crate stores tracks.file_path relative to the library directory for files
// it organized itself; importers may store absolute paths anywhere on disk.
// Everything that touches a stored path must resolve it the same way, and
// destructive operations must stay inside the library directory.
package library

import (
	"path/filepath"
	"strings"
)

// ResolvePath returns the absolute location of a stored track file path:
// relative paths are joined onto the library directory, absolute paths are
// used as-is.
func ResolvePath(libraryDir, p string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(libraryDir, p)
	}
	return filepath.Clean(p)
}

// Contains reports whether abs sits strictly inside libraryDir. The library
// root itself does not count as contained.
func Contains(libraryDir, abs string) bool {
	rel, err := filepath.Rel(filepath.Clean(libraryDir), filepath.Clean(abs))
	return err == nil && rel != "." && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
