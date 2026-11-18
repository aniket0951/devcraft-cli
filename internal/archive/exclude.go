package archive

import (
	"path"
	"strings"
)

// Directories excluded anywhere in the workspace.
var excludedDirsAnywhere = map[string]bool{
	".git":          true,
	".hg":           true,
	".svn":          true,
	".devcraft":     true,
	"node_modules":  true,
	"__pycache__":   true,
	".venv":         true,
	"venv":          true,
	".idea":         true,
	".vscode":       true,
	".cache":        true,
	".gradle":       true,
	".pytest_cache": true,
}

// Build output / temp directories excluded only at the workspace root, since
// names like "build" can be legitimate source packages deeper in the tree.
var excludedDirsAtRoot = map[string]bool{
	"bin":    true,
	"dist":   true,
	"build":  true,
	"out":    true,
	"target": true,
	"tmp":    true,
	"temp":   true,
}

// Exact file names that are excluded.
var excludedFiles = map[string]bool{
	".DS_Store":   true,
	"Thumbs.db":   true,
	".netrc":      true,
	".npmrc":      true,
	".pypirc":     true,
	"id_rsa":      true,
	"id_dsa":      true,
	"id_ecdsa":    true,
	"id_ed25519":  true,
	"credentials": true,
}

// File glob patterns that are excluded.
var excludedPatterns = []string{
	".env",
	".env.*",
	"*.pem",
	"*.key",
	"*.p12",
	"*.pfx",
	"*.swp",
	"*.swo",
	"*~",
	"*.tmp",
	"*.log",
	"*.test",
	"*.exe",
	"*.prof",
	"*.coverprofile",
	"coverage.out",
}

// Files that match excludedPatterns but are safe templates.
var allowedFiles = map[string]bool{
	".env.example":  true,
	".env.sample":   true,
	".env.template": true,
}

// Excluded reports whether relPath (slash-separated, relative to the
// workspace root) should be left out of the archive.
func Excluded(relPath string, isDir bool) bool {
	name := path.Base(relPath)
	if isDir {
		if excludedDirsAnywhere[name] {
			return true
		}
		return !strings.Contains(relPath, "/") && excludedDirsAtRoot[name]
	}
	if allowedFiles[name] {
		return false
	}
	if excludedFiles[name] {
		return true
	}
	for _, p := range excludedPatterns {
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}
