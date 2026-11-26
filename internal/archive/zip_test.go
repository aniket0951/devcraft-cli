package archive

import (
	"archive/zip"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type zipEntry struct {
	name    string
	content string
	mode    fs.FileMode // 0 means a regular 0644 file
}

func makeZip(t *testing.T, entries ...zipEntry) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		if strings.HasSuffix(e.name, "/") {
			mode = fs.ModeDir | 0o755
		}
		h.SetMode(mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func extractZip(t *testing.T, r *bytes.Reader, limits Limits) (string, error) {
	t.Helper()
	dest := t.TempDir()
	return dest, ExtractZip(r, r.Size(), dest, limits)
}

// tree lists every file under root as slash paths.
func tree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return err
	})
	slices.Sort(out)
	return out
}

func TestExtractZip_FlatArchive(t *testing.T) {
	dest, err := extractZip(t, makeZip(t,
		zipEntry{name: "README.md", content: "# Starter"},
		zipEntry{name: "cmd/"},
		zipEntry{name: "cmd/main.go", content: "package main\n"},
		zipEntry{name: "run.sh", content: "#!/bin/sh\n", mode: 0o755},
	), DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := tree(t, dest), []string{"README.md", "cmd/main.go", "run.sh"}; !slices.Equal(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "cmd", "main.go")); string(b) != "package main\n" {
		t.Fatalf("content = %q", b)
	}
	if info, _ := os.Stat(filepath.Join(dest, "run.sh")); info.Mode().Perm()&0o111 == 0 {
		t.Fatal("executable bit lost")
	}
}

func TestExtractZip_StripsSingleRootAndMacMetadata(t *testing.T) {
	dest, err := extractZip(t, makeZip(t,
		zipEntry{name: "GO-SRP-01/"},
		zipEntry{name: "GO-SRP-01/go.mod", content: "module x\n"},
		zipEntry{name: "GO-SRP-01/internal/order.go", content: "package internal\n"},
		zipEntry{name: "GO-SRP-01/.DS_Store", content: "junk"},
		zipEntry{name: "__MACOSX/GO-SRP-01/._go.mod", content: "junk"},
	), DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := tree(t, dest), []string{"go.mod", "internal/order.go"}; !slices.Equal(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
}

func TestExtractZip_KeepsSeveralTopLevelDirs(t *testing.T) {
	dest, err := extractZip(t, makeZip(t,
		zipEntry{name: "cmd/main.go", content: "package main"},
		zipEntry{name: "internal/x.go", content: "package internal"},
	), DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := tree(t, dest), []string{"cmd/main.go", "internal/x.go"}; !slices.Equal(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
}

func TestExtractZip_RejectsMaliciousEntries(t *testing.T) {
	for name, entry := range map[string]zipEntry{
		"traversal":         {name: "../evil.txt", content: "x"},
		"nested traversal":  {name: "root/../../evil.txt", content: "x"},
		"absolute":          {name: "/etc/evil", content: "x"},
		"windows traversal": {name: `..\evil.txt`, content: "x"},
		"symlink":           {name: "link", content: "/etc/passwd", mode: fs.ModeSymlink | 0o777},
	} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			dest := filepath.Join(parent, "ws")
			_ = os.Mkdir(dest, 0o755)
			r := makeZip(t, zipEntry{name: "ok.txt", content: "fine"}, entry)
			if err := ExtractZip(r, r.Size(), dest, DefaultLimits); err == nil {
				t.Fatal("expected error")
			}
			if _, err := os.Stat(filepath.Join(parent, "evil.txt")); !os.IsNotExist(err) {
				t.Fatal("file written outside dest")
			}
		})
	}
}

func TestExtractZip_Limits(t *testing.T) {
	small := Limits{MaxFiles: 2, MaxFileBytes: 10, MaxTotalBytes: 15}

	_, err := extractZip(t, makeZip(t,
		zipEntry{name: "a", content: "1"}, zipEntry{name: "b", content: "2"}, zipEntry{name: "c", content: "3"},
	), small)
	if !errors.Is(err, ErrLimitExceeded) {
		t.Errorf("too many files: err = %v", err)
	}

	_, err = extractZip(t, makeZip(t, zipEntry{name: "big", content: strings.Repeat("x", 11)}), small)
	if !errors.Is(err, ErrLimitExceeded) {
		t.Errorf("file too large: err = %v", err)
	}

	_, err = extractZip(t, makeZip(t,
		zipEntry{name: "a", content: strings.Repeat("x", 8)}, zipEntry{name: "b", content: strings.Repeat("x", 8)},
	), small)
	if !errors.Is(err, ErrLimitExceeded) {
		t.Errorf("total too large: err = %v", err)
	}
}

func TestExtractZip_DuplicateEntryDoesNotOverwrite(t *testing.T) {
	_, err := extractZip(t, makeZip(t,
		zipEntry{name: "main.go", content: "first"}, zipEntry{name: "main.go", content: "second"},
	), DefaultLimits)
	if err == nil {
		t.Fatal("expected error for duplicate entry")
	}
}

func TestExtractZip_NotAZip(t *testing.T) {
	r := bytes.NewReader([]byte("definitely not a zip"))
	if err := ExtractZip(r, r.Size(), t.TempDir(), DefaultLimits); err == nil {
		t.Fatal("expected error")
	}
}
