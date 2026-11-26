package archive

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

// ExtractZip safely unpacks the ZIP in r (size bytes) into dest, which must
// already exist. It applies the same rules as Extract: only regular files and
// directories, no absolute paths or ".." components, no overwrites, and
// limits on file count and uncompressed size (checked against the bytes
// actually read, not the sizes the ZIP declares).
//
// macOS metadata (__MACOSX/, .DS_Store) is skipped. If every entry is inside
// one top-level directory (a zipped folder), that directory is stripped so
// its contents land directly in dest.
func ExtractZip(r io.ReaderAt, size int64, dest string, limits Limits) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return fmt.Errorf("read archive: %w", err)
	}

	// Validate every name before stripping, so a "../" can't be hidden by it.
	var entries []*zip.File
	for _, f := range zr.File {
		if _, err := safeJoin(dest, f.Name); err != nil {
			return err
		}
		if !isMacMetadata(f.Name) {
			entries = append(entries, f)
		}
	}
	root := commonRoot(entries)

	var files int
	var total int64
	for _, f := range entries {
		name := zipName(f.Name)
		if root != "" {
			name = strings.TrimPrefix(name, root+"/")
		}
		if name == "" || name == "." {
			continue // the stripped root itself
		}
		target, err := safeJoin(dest, name)
		if err != nil {
			return err
		}

		mode := f.Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case mode.IsRegular():
			files++
			if files > limits.MaxFiles {
				return fmt.Errorf("%w: more than %d files", ErrLimitExceeded, limits.MaxFiles)
			}
			if f.UncompressedSize64 > uint64(limits.MaxFileBytes) {
				return fmt.Errorf("%w: %s is %d bytes (max %d per file)", ErrLimitExceeded, f.Name, f.UncompressedSize64, limits.MaxFileBytes)
			}
			declared := int64(f.UncompressedSize64)
			total += declared
			if total > limits.MaxTotalBytes {
				return fmt.Errorf("%w: archive expands to more than %d bytes", ErrLimitExceeded, limits.MaxTotalBytes)
			}
			if err := extractZipFile(f, target, declared, mode); err != nil {
				return err
			}
		default:
			return fmt.Errorf("archive entry %q has unsupported type %s (links and special files are not allowed)", f.Name, mode.Type())
		}
	}
	return nil
}

// extractZipFile writes one entry. writeFile reads at most declared+1 bytes
// and fails on any mismatch, so a ZIP that lies about its sizes can't expand
// past the limits.
func extractZipFile(f *zip.File, target string, declared int64, mode fs.FileMode) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("read archive entry %q: %w", f.Name, err)
	}
	defer rc.Close()
	return writeFile(target, rc, f.Name, declared, mode&0o111 != 0)
}

// zipName normalises an entry name to forward slashes without a leading "./"
// or trailing "/".
func zipName(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	for strings.HasPrefix(name, "./") {
		name = name[2:]
	}
	return strings.TrimSuffix(name, "/")
}

// commonRoot returns the single top-level directory that contains every
// entry, or "" if there are top-level files or several top-level entries.
func commonRoot(entries []*zip.File) string {
	root := ""
	for _, f := range entries {
		name := zipName(f.Name)
		first, _, nested := strings.Cut(name, "/")
		if !nested && !f.Mode().IsDir() {
			return "" // a file at the top level
		}
		if root == "" {
			root = first
		} else if first != root {
			return ""
		}
	}
	return root
}

// isMacMetadata reports Finder's resource-fork and folder-view files.
func isMacMetadata(name string) bool {
	name = zipName(name)
	return name == "__MACOSX" || strings.HasPrefix(name, "__MACOSX/") || path.Base(name) == ".DS_Store"
}
