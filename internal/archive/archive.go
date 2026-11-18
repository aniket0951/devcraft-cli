// Package archive creates deterministic workspace archives and safely extracts
// starter archives. Format: gzip-compressed tar containing regular files only.
package archive

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Limits bounds archive size.
type Limits struct {
	MaxFiles      int
	MaxFileBytes  int64
	MaxTotalBytes int64 // total uncompressed bytes
}

// DefaultLimits apply to workspace archives and starter extraction.
var DefaultLimits = Limits{
	MaxFiles:      2000,
	MaxFileBytes:  10 << 20,
	MaxTotalBytes: 50 << 20,
}

// ErrLimitExceeded is wrapped by errors caused by Limits.
var ErrLimitExceeded = errors.New("archive limit exceeded")

// Result describes a created archive.
type Result struct {
	Checksum  string // hex SHA-256 of the compressed archive
	Size      int64  // compressed size in bytes
	FileCount int
	RawBytes  int64     // total uncompressed file bytes
	Skipped   []string  // symlinks and special files that were not included
	Secrets   []Finding // likely secrets in included files
}

// epoch is the fixed modification time written for every entry so the same
// content always produces the same archive bytes.
var epoch = time.Unix(0, 0).UTC()

// Create writes a deterministic tar.gz of root to w. Excluded paths are
// skipped, symlinks are never followed, and limits are enforced.
func Create(root string, w io.Writer, limits Limits) (*Result, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	hasher := sha256.New()
	counter := &countingWriter{}
	gz, err := gzip.NewWriterLevel(io.MultiWriter(w, hasher, counter), gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	tw := tar.NewWriter(gz)
	res := &Result{}

	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			if Excluded(rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if Excluded(rel, false) {
			return nil
		}
		// Symlinks, sockets, devices etc. are never followed or archived.
		if !d.Type().IsRegular() {
			res.Skipped = append(res.Skipped, rel)
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > limits.MaxFileBytes {
			return fmt.Errorf("%w: %s is %d bytes (max %d per file)", ErrLimitExceeded, rel, info.Size(), limits.MaxFileBytes)
		}
		res.FileCount++
		if res.FileCount > limits.MaxFiles {
			return fmt.Errorf("%w: more than %d files", ErrLimitExceeded, limits.MaxFiles)
		}

		content, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		// Re-check against what was actually read, in case the file grew.
		size := int64(len(content))
		if size > limits.MaxFileBytes {
			return fmt.Errorf("%w: %s is %d bytes (max %d per file)", ErrLimitExceeded, rel, size, limits.MaxFileBytes)
		}
		res.RawBytes += size
		if res.RawBytes > limits.MaxTotalBytes {
			return fmt.Errorf("%w: workspace is larger than %d bytes", ErrLimitExceeded, limits.MaxTotalBytes)
		}
		for _, reason := range scanSecrets(content) {
			res.Secrets = append(res.Secrets, Finding{Path: rel, Reason: reason})
		}

		mode := int64(0o644)
		if info.Mode().Perm()&0o111 != 0 {
			mode = 0o755
		}
		hdr := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     rel,
			Size:     size,
			Mode:     mode,
			ModTime:  epoch,
			Format:   tar.FormatPAX,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err = tw.Write(content)
		return err
	})
	if walkErr != nil {
		return nil, walkErr
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}

	res.Checksum = hex.EncodeToString(hasher.Sum(nil))
	res.Size = counter.n
	return res, nil
}

// Extract safely unpacks a tar.gz from r into dest, which must already exist.
// Only regular files and directories are allowed; absolute paths, ".."
// components, links and special files are rejected; limits are enforced.
func Extract(r io.Reader, dest string, limits Limits) error {
	dest, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("read archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	var files int
	var total int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}

		target, err := safeJoin(dest, hdr.Name)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			files++
			if files > limits.MaxFiles {
				return fmt.Errorf("%w: more than %d files", ErrLimitExceeded, limits.MaxFiles)
			}
			if hdr.Size > limits.MaxFileBytes {
				return fmt.Errorf("%w: %s is %d bytes (max %d per file)", ErrLimitExceeded, hdr.Name, hdr.Size, limits.MaxFileBytes)
			}
			total += hdr.Size
			if total > limits.MaxTotalBytes {
				return fmt.Errorf("%w: archive expands to more than %d bytes", ErrLimitExceeded, limits.MaxTotalBytes)
			}
			if err := writeFile(target, tr, hdr); err != nil {
				return err
			}
		case tar.TypeXGlobalHeader:
			// PAX global header carries no file; ignore.
		default:
			return fmt.Errorf("archive entry %q has unsupported type %q (links and special files are not allowed)", hdr.Name, string(hdr.Typeflag))
		}
	}
}

func writeFile(target string, r io.Reader, hdr *tar.Header) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if hdr.Mode&0o111 != 0 {
		mode = 0o755
	}
	// O_EXCL: never overwrite (or write through) anything already present.
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, hdr.Size+1))
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if n != hdr.Size {
		return fmt.Errorf("archive entry %q: size mismatch", hdr.Name)
	}
	return closeErr
}

// safeJoin resolves name inside dest, rejecting anything that would escape it.
func safeJoin(dest, name string) (string, error) {
	if name == "" || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("archive entry has invalid name %q", name)
	}
	slashed := strings.ReplaceAll(name, `\`, "/")
	if path.IsAbs(slashed) || filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return "", fmt.Errorf("archive entry %q has an absolute path", name)
	}
	if slices.Contains(strings.Split(slashed, "/"), "..") {
		return "", fmt.Errorf("archive entry %q escapes the destination", name)
	}
	target := filepath.Join(dest, filepath.FromSlash(path.Clean(slashed)))
	if target != dest && !strings.HasPrefix(target, dest+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry %q escapes the destination", name)
	}
	return target, nil
}

type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}
