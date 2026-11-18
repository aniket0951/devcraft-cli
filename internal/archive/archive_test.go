package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func listEntries(t *testing.T, data []byte) []string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var names []string
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return names
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
	}
}

func TestExcluded(t *testing.T) {
	cases := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{".git", true, true},
		{".devcraft", true, true},
		{"web/node_modules", true, true},
		{"bin", true, true},
		{"internal/build", true, false}, // only excluded at root
		{"build", true, true},
		{".env", false, true},
		{"config/.env.local", false, true},
		{".env.example", false, false},
		{"server.pem", false, true},
		{"notes.log", false, true},
		{"main.go", false, false},
		{"README.md", false, false},
		{".DS_Store", false, true},
	}
	for _, c := range cases {
		if got := Excluded(c.path, c.isDir); got != c.want {
			t.Errorf("Excluded(%q, %v) = %v, want %v", c.path, c.isDir, got, c.want)
		}
	}
}

func TestCreate_ExclusionsAndContents(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"main.go":                   "package main",
		"internal/x/x.go":           "package x",
		".env":                      "SECRET=1",
		".env.example":              "SECRET=",
		".git/config":               "[core]",
		".devcraft/assignment.json": "{}",
		"node_modules/a/index.js":   "x",
		"bin/app":                   "binary",
	})

	var buf bytes.Buffer
	res, err := Create(root, &buf, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	got := listEntries(t, buf.Bytes())
	want := []string{".env.example", "internal/x/x.go", "main.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	if res.FileCount != 3 || res.Size != int64(buf.Len()) || len(res.Checksum) != 64 {
		t.Fatalf("result = %+v", res)
	}
}

func TestCreate_Deterministic(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"a.go": "a", "b/c.go": "c"})

	var first, second bytes.Buffer
	r1, err := Create(root, &first, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	// Changing mtimes must not change the archive.
	later := time.Now().Add(time.Hour)
	_ = os.Chtimes(filepath.Join(root, "a.go"), later, later)

	r2, err := Create(root, &second, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Checksum != r2.Checksum || !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("archives differ for identical content")
	}

	writeFiles(t, root, map[string]string{"a.go": "changed"})
	var third bytes.Buffer
	r3, _ := Create(root, &third, DefaultLimits)
	if r3.Checksum == r1.Checksum {
		t.Fatal("checksum did not change when content changed")
	}
}

func TestCreate_SkipsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	writeFiles(t, root, map[string]string{"main.go": "package main"})
	writeFiles(t, outside, map[string]string{"secret.txt": "top secret"})
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linkdir")); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	res, err := Create(root, &buf, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if got := listEntries(t, buf.Bytes()); !slices.Equal(got, []string{"main.go"}) {
		t.Fatalf("entries = %v", got)
	}
	if !slices.Equal(res.Skipped, []string{"link.txt", "linkdir"}) {
		t.Fatalf("skipped = %v", res.Skipped)
	}
}

func TestCreate_Limits(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"a": "12345", "b": "12345", "c": "12345"})

	cases := map[string]Limits{
		"files":    {MaxFiles: 2, MaxFileBytes: 100, MaxTotalBytes: 100},
		"fileSize": {MaxFiles: 10, MaxFileBytes: 4, MaxTotalBytes: 100},
		"total":    {MaxFiles: 10, MaxFileBytes: 100, MaxTotalBytes: 12},
	}
	for name, lim := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Create(root, io.Discard, lim)
			if !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("err = %v, want ErrLimitExceeded", err)
			}
		})
	}
}

func TestCreate_DetectsSecrets(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"main.go":    "package main",
		"config.go":  `const key = "AKIAABCDEFGHIJKLMNOP"`,
		"deploy.txt": "-----BEGIN RSA PRIVATE KEY-----\nabc\n",
	})
	res, err := Create(root, io.Discard, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range res.Secrets {
		paths = append(paths, f.Path)
	}
	if !slices.Equal(paths, []string{"config.go", "deploy.txt"}) {
		t.Fatalf("secret findings = %+v", res.Secrets)
	}
}

func TestExtract_RoundTrip(t *testing.T) {
	src := t.TempDir()
	writeFiles(t, src, map[string]string{"main.go": "package main", "dir/sub/f.txt": "hello"})
	if err := os.Chmod(filepath.Join(src, "main.go"), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := Create(src, &buf, DefaultLimits); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	if err := Extract(&buf, dest, DefaultLimits); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "dir", "sub", "f.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("content = %q err = %v", got, err)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(filepath.Join(dest, "main.go"))
		if info.Mode().Perm() != 0o755 {
			t.Fatalf("mode = %v, want 0755", info.Mode().Perm())
		}
	}
}

// buildTar makes a tar.gz with arbitrary headers for malicious-input tests.
func buildTar(t *testing.T, entries ...tar.Header) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range entries {
		body := strings.Repeat("x", int(h.Size))
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return &buf
}

func TestExtract_RejectsMaliciousEntries(t *testing.T) {
	cases := map[string]tar.Header{
		"parent traversal": {Name: "../evil.txt", Typeflag: tar.TypeReg, Size: 1, Mode: 0o644},
		"nested traversal": {Name: "a/../../evil.txt", Typeflag: tar.TypeReg, Size: 1, Mode: 0o644},
		"absolute path":    {Name: "/tmp/evil.txt", Typeflag: tar.TypeReg, Size: 1, Mode: 0o644},
		"backslash escape": {Name: `..\evil.txt`, Typeflag: tar.TypeReg, Size: 1, Mode: 0o644},
		"symlink":          {Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
		"hardlink":         {Name: "hard", Typeflag: tar.TypeLink, Linkname: "/etc/passwd"},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			dest := filepath.Join(parent, "dest")
			_ = os.Mkdir(dest, 0o755)

			if err := Extract(buildTar(t, h), dest, DefaultLimits); err == nil {
				t.Fatal("expected error")
			}
			if _, err := os.Lstat(filepath.Join(parent, "evil.txt")); err == nil {
				t.Fatal("file written outside destination")
			}
		})
	}
}

func TestExtract_Limits(t *testing.T) {
	lim := Limits{MaxFiles: 1, MaxFileBytes: 10, MaxTotalBytes: 10}

	tooMany := buildTar(t,
		tar.Header{Name: "a", Typeflag: tar.TypeReg, Size: 1, Mode: 0o644},
		tar.Header{Name: "b", Typeflag: tar.TypeReg, Size: 1, Mode: 0o644},
	)
	if err := Extract(tooMany, t.TempDir(), lim); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("too many files: err = %v", err)
	}

	tooBig := buildTar(t, tar.Header{Name: "a", Typeflag: tar.TypeReg, Size: 11, Mode: 0o644})
	if err := Extract(tooBig, t.TempDir(), lim); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("too big: err = %v", err)
	}
}

func TestExtract_DoesNotOverwrite(t *testing.T) {
	dest := t.TempDir()
	writeFiles(t, dest, map[string]string{"a": "original"})
	err := Extract(buildTar(t, tar.Header{Name: "a", Typeflag: tar.TypeReg, Size: 1, Mode: 0o644}), dest, DefaultLimits)
	if err == nil {
		t.Fatal("expected error when file exists")
	}
	got, _ := os.ReadFile(filepath.Join(dest, "a"))
	if string(got) != "original" {
		t.Fatal("existing file was overwritten")
	}
}
