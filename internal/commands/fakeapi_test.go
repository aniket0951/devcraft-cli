package commands

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aniket0951/devcraft-cli.git/internal/auth"
	"github.com/aniket0951/devcraft-cli.git/internal/workspace"
	"github.com/aniket0951/devcraft-cli.git/pkg/client"
)

// fakeAPI is an in-memory stand-in for the DevCraft backend.
type fakeAPI struct {
	t   *testing.T
	srv *httptest.Server

	mu              sync.Mutex
	starter         []byte
	starterChecksum string
	attemptsCreated int
	uploads         map[string][]byte // artifact ID -> archive
	uploadAuth      []string          // Authorization headers seen on uploads
	evaluation      client.Evaluation // returned for every evaluation
	evalPolls       int               // evaluations report "running" this many times first
	latestEvalID    string
	latestSubID     string
	unauthorized    bool
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{
		t:       t,
		uploads: map[string][]byte{},
		evaluation: client.Evaluation{
			ID: "eval_1", Kind: "quick", Status: client.EvaluationCompleted, Passed: true,
			Score: 10, MaxScore: 10,
			Checks: []client.Check{{Name: "build", Status: client.CheckPassed}},
		},
	}
	f.starter = makeTarGz(t, map[string]string{"README.md": "# Starter", "main.go": "package main\n"})
	sum := sha256.Sum256(f.starter)
	f.starterChecksum = hex.EncodeToString(sum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/assignments", f.authed(func(w http.ResponseWriter, r *http.Request) {
		items := []client.Assignment{
			{ID: "GO-SOLID-001", Title: "SOLID in Go", Language: "go", Category: "design", Difficulty: "medium"},
			{ID: "GO-CONC-002", Title: "Concurrency", Language: "go", Category: "concurrency", Difficulty: "hard"},
		}
		if lang := r.URL.Query().Get("language"); lang != "" && lang != "go" {
			items = nil
		}
		writeJSON(w, http.StatusOK, client.AssignmentList{Items: items, NextCursor: r.URL.Query().Get("limit")})
	}))
	mux.HandleFunc("POST /v1/attempts", f.authed(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["assignment_id"] != "GO-SOLID-001" {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]string{"code": "not_found", "message": "assignment not found"}})
			return
		}
		f.mu.Lock()
		f.attemptsCreated++
		f.mu.Unlock()
		writeJSON(w, http.StatusCreated, client.Attempt{
			ID: "att_1", AssignmentID: "GO-SOLID-001", AssignmentVersion: 3, Status: "in_progress",
			StarterURL: f.srv.URL + "/storage/starter.tar.gz", StarterChecksum: f.starterChecksum,
		})
	}))
	mux.HandleFunc("GET /v1/attempts/{id}", f.authed(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		writeJSON(w, http.StatusOK, client.Attempt{
			ID: r.PathValue("id"), AssignmentID: "GO-SOLID-001", AssignmentVersion: 3, Status: "in_progress",
			LatestEvaluationID: f.latestEvalID, LatestSubmissionID: f.latestSubID,
		})
	}))
	mux.HandleFunc("GET /storage/starter.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(f.starter)
	})
	mux.HandleFunc("POST /v1/artifacts/upload-url", f.authed(func(w http.ResponseWriter, r *http.Request) {
		var req client.UploadURLRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.AttemptID != "att_1" || req.Checksum == "" || req.Size <= 0 {
			t.Errorf("bad upload-url request: %+v", req)
		}
		id := "art_" + string(req.Purpose)
		writeJSON(w, http.StatusOK, client.UploadURL{
			ArtifactID: id, URL: f.srv.URL + "/storage/upload/" + id,
			ExpiresAt: time.Now().Add(time.Minute),
		})
	}))
	mux.HandleFunc("PUT /storage/upload/{id}", func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.uploads[r.PathValue("id")] = data
		f.uploadAuth = append(f.uploadAuth, r.Header.Get("Authorization"))
		f.mu.Unlock()
	})
	mux.HandleFunc("POST /v1/evaluations/quick", f.authed(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.latestEvalID = f.evaluation.ID
		writeJSON(w, http.StatusCreated, client.Evaluation{ID: f.evaluation.ID, Kind: "quick", Status: client.EvaluationQueued})
	}))
	mux.HandleFunc("POST /v1/attempts/{id}/submissions", f.authed(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		// Verify the checksum against what was uploaded, like the real API should.
		sum := sha256.Sum256(f.uploads[body["artifact_id"]])
		if hex.EncodeToString(sum[:]) != body["checksum"] {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "checksum_mismatch"}})
			return
		}
		f.latestSubID = "sub_1"
		f.latestEvalID = f.evaluation.ID
		writeJSON(w, http.StatusCreated, client.Submission{ID: "sub_1", AttemptID: r.PathValue("id"), Status: "queued", CreatedAt: time.Now()})
	}))
	mux.HandleFunc("GET /v1/submissions/{id}", f.authed(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, client.Submission{ID: r.PathValue("id"), Status: "evaluated", EvaluationID: f.evaluation.ID, CreatedAt: time.Now()})
	}))
	mux.HandleFunc("GET /v1/evaluations/{id}", f.authed(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.evalPolls > 0 {
			f.evalPolls--
			writeJSON(w, http.StatusOK, client.Evaluation{ID: r.PathValue("id"), Status: client.EvaluationRunning})
			return
		}
		writeJSON(w, http.StatusOK, f.evaluation)
	}))

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) authed(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		unauthorized := f.unauthorized
		f.mu.Unlock()
		if unauthorized || r.Header.Get("Authorization") != "Bearer test-token" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "unauthorized"}})
			return
		}
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func makeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		_ = tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Size: int64(len(content)), Mode: 0o644})
		_, _ = tw.Write([]byte(content))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func tarNames(t *testing.T, data []byte) []string {
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

// loginForTest stores a session so commands are authenticated.
func loginForTest(t *testing.T) {
	t.Helper()
	setupTestEnv(t)
	if err := auth.NewAuthHandler().SaveLogin(auth.Session{IDToken: "test-token", Email: "user@example.com"}); err != nil {
		t.Fatal(err)
	}
}

// runCLI executes the root command with args and returns combined output.
func runCLI(t *testing.T, f *fakeAPI, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if f != nil {
		args = append(args, "--api-url", f.srv.URL)
	}
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// fastPolling shortens polling for tests.
func fastPolling(t *testing.T) {
	t.Helper()
	oldI, oldT := pollInterval, pollTimeout
	pollInterval, pollTimeout = 5*time.Millisecond, 5*time.Second
	t.Cleanup(func() { pollInterval, pollTimeout = oldI, oldT })
}

// newWorkspace creates a workspace with metadata and files, and chdirs into it.
func newWorkspace(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := workspace.Write(root, workspace.Metadata{AssignmentID: "GO-SOLID-001", AssignmentVersion: 3, AttemptID: "att_1"}); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	return root
}

func assertContains(t *testing.T, s string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			t.Errorf("output missing %q\n--- output ---\n%s", sub, s)
		}
	}
}
