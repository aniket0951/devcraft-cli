package commands

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"cmp"
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

	"github.com/aniket0951/devcraft-cli.git/internal/config"

	"github.com/aniket0951/devcraft-cli.git/internal/auth"
	"github.com/aniket0951/devcraft-cli.git/internal/workspace"
	"github.com/aniket0951/devcraft-cli.git/pkg/client"
)

// fakeAPI is an in-memory stand-in for the DevCraft backend.
type fakeAPI struct {
	t   *testing.T
	srv *httptest.Server

	mu              sync.Mutex
	starter         []byte            // starter ZIP served at download_url
	starterChecksum string            // sha256_hex the API reports
	starterSize     int64             // size_bytes the API reports
	downloadCalls   int               // authorised GET /v1/assignments/{code}/download requests
	requests        int               // every request to an authenticated route, accepted or not
	downloadURL     string            // overrides the starter download_url when set
	storage         http.HandlerFunc  // overrides how the starter ZIP is served when set
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
	f.starter = makeZip(t, map[string]string{
		"GO-SOLID-001/README.md": "# Starter",
		"GO-SOLID-001/main.go":   "package main\n",
	})
	sum := sha256.Sum256(f.starter)
	f.starterChecksum = hex.EncodeToString(sum[:])
	f.starterSize = int64(len(f.starter))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/assignments", f.authed(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		items := []client.Assignment{}
		for _, a := range []client.Assignment{
			{Code: "GO-CONC-002", Name: "Worker pool", Language: "go", CategoryName: "Concurrency", Level: "L2", CurrentVersion: "1.0.0"},
			{Code: "GO-SOLID-001", Name: "SOLID in Go", Language: "go", CategoryName: "SOLID Principles", Level: "L1", CurrentVersion: "1.2.0"},
		} {
			if (q.Get("language") == "" || q.Get("language") == a.Language) &&
				(q.Get("category") == "" || strings.EqualFold(q.Get("category"), a.CategoryName)) &&
				(q.Get("level") == "" || strings.EqualFold(q.Get("level"), a.Level)) {
				items = append(items, a)
			}
		}
		writeJSON(w, http.StatusOK, items)
	}))
	mux.HandleFunc("GET /v1/assignments/{code}/download", f.authed(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.downloadCalls++
		if strings.ToUpper(r.PathValue("code")) != "GO-SOLID-001" {
			writeAPIError(w, http.StatusNotFound, "NOT_FOUND_ERR", "assignment not found")
			return
		}
		writeJSON(w, http.StatusOK, client.AssignmentDownload{
			AssignmentCode: "GO-SOLID-001", Version: "1.2.0",
			DownloadURL: cmp.Or(f.downloadURL, f.srv.URL+"/storage/starter.zip?X-Amz-Signature=secret"),
			SizeBytes:   f.starterSize, SHA256Hex: f.starterChecksum,
			ExpiresAt: time.Now().Add(5 * time.Minute),
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
	mux.HandleFunc("GET /storage/starter.zip", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("API token must not be sent to the download URL")
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.storage != nil {
			f.storage(w, r)
			return
		}
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
			writeAPIError(w, http.StatusBadRequest, "UNPROCESSABLE_ERR", "checksum mismatch")
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
		f.requests++
		unauthorized := f.unauthorized
		f.mu.Unlock()
		if unauthorized || r.Header.Get("Authorization") != "Bearer test-token" {
			writeAPIError(w, http.StatusUnauthorized, "UNAUTHORIZED_ERR", "invalid token")
			return
		}
		h(w, r)
	}
}

// writeJSON writes v as the data of a success envelope, like the real API.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"status_code": status, "message": "ok", "data": v})
}

// writeAPIError writes the API's error envelope.
func writeAPIError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status_code": status, "message": msg,
		"error": map[string]string{"err_code": code, "err_msg": msg, "debug_mes": msg},
	})
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		fw, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
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
	if err := workspace.Write(root, workspace.Metadata{AssignmentID: "GO-SOLID-001", AssignmentVersion: "1.2.0", AttemptID: "att_1"}); err != nil {
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

// chdirAssignments makes a temp folder the assignments folder (through
// DEVCRAFT_ASSIGNMENTS_DIR) and the current directory, and returns it.
func chdirAssignments(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(config.EnvAssignmentsDir, dir)
	t.Chdir(dir)
	return dir
}
