package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "tok-123", WithUserAgent("devcraft-test"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNew_RejectsBadScheme(t *testing.T) {
	if _, err := New("ftp://example.com", "t"); err == nil {
		t.Fatal("expected error for ftp scheme")
	}
}

func TestRequestHeadersAndQuery(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/assignments" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-123" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "devcraft-test" {
			t.Errorf("User-Agent = %q", got)
		}
		q := r.URL.Query()
		if q.Get("language") != "go" || q.Get("category") != "SOLID Principles" || q.Get("level") != "L1" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"status_code":200,"message":"assignments","data":[{"assignment_pid":"asg_1",
			"assignment_code":"GO-SRP-01","name":"Order processor","language":"go","category_name":"SOLID Principles",
			"level":"L1","status":"published","current_version":"1.0.0","is_active":true}]}`)
	})

	got, err := c.ListAssignments(context.Background(), ListAssignmentsParams{Language: "go", Category: "SOLID Principles", Level: "L1"})
	if err != nil {
		t.Fatal(err)
	}
	want := Assignment{Code: "GO-SRP-01", Name: "Order processor", Language: "go", CategoryName: "SOLID Principles", Level: "L1", CurrentVersion: "1.0.0"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v", got)
	}
}

func TestPathEscaping(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/v1/attempts/a%2Fb" {
			t.Errorf("escaped path = %s", r.URL.EscapedPath())
		}
		writeData(w, http.StatusOK, Attempt{ID: "a/b"})
	})
	if _, err := c.GetAttempt(context.Background(), "a/b"); err != nil {
		t.Fatal(err)
	}
}

func TestPostBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/attempts/att_1/submissions" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["artifact_id"] != "art_1" || body["checksum"] != "abc" {
			t.Errorf("body = %v", body)
		}
		writeData(w, http.StatusCreated, Submission{ID: "sub_1"})
	})
	sub, err := c.CreateSubmission(context.Background(), "att_1", "art_1", "abc")
	if err != nil || sub.ID != "sub_1" {
		t.Fatalf("sub=%+v err=%v", sub, err)
	}
}

func TestAPIErrorParsing(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"status_code":401,"message":"unauthorized","error":{"err_code":"UNAUTHORIZED_ERR","err_msg":"session expired","debug_mes":"x"}}`)
	})
	_, err := c.GetEvaluation(context.Background(), "e1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %T %v", err, err)
	}
	if apiErr.Code != "UNAUTHORIZED_ERR" || apiErr.Message != "session expired" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if !IsUnauthorized(err) || IsNotFound(err) {
		t.Fatal("status helpers wrong")
	}
}

func TestAPIErrorFallsBackToTopLevelMessage(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"status_code":404,"message":"assignment not found"}`)
	})
	_, err := c.GetAssignment(context.Background(), "GO-1")
	if !IsNotFound(err) || !strings.Contains(err.Error(), "assignment not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestDecode_MissingData(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status_code":200,"message":"ok"}`)
	})
	if _, err := c.GetAssignment(context.Background(), "GO-1"); err == nil || !strings.Contains(err.Error(), "no data") {
		t.Fatalf("err = %v, want no-data error", err)
	}
}

func TestGetAssignmentDownload(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/assignments/GO-SRP-01/download" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"status_code":200,"message":"download","data":{
			"assignment_code":"GO-SRP-01","version":"1.0.0","download_url":"https://s3.example/x?sig=1",
			"size_bytes":9497,"sha256_hex":"ab","expires_at":"2026-11-26T20:00:00Z","expires_in_seconds":300}}`)
	})
	got, err := c.GetAssignmentDownload(context.Background(), "GO-SRP-01")
	if err != nil {
		t.Fatal(err)
	}
	if got.AssignmentCode != "GO-SRP-01" || got.Version != "1.0.0" || got.SizeBytes != 9497 ||
		got.SHA256Hex != "ab" || got.DownloadURL != "https://s3.example/x?sig=1" || got.ExpiresAt.IsZero() {
		t.Fatalf("got %+v", got)
	}
}

func TestDownload_ErrorHidesURL(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {})
	secret := "http://127.0.0.1:1/starter?X-Amz-Signature=secret"
	_, err := c.Download(context.Background(), secret, io.Discard, 10)
	if err == nil {
		t.Fatal("expected connection error")
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "127.0.0.1:1/starter") {
		t.Fatalf("error leaks the pre-signed URL: %v", err)
	}
}

func TestAPIErrorNonJSONBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	_, err := c.GetEvaluation(context.Background(), "e1")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v", err)
	}
}

func TestUploadArtifact_NoAuthHeader(t *testing.T) {
	var got []byte
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s", r.Method)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("API token must not be sent to the upload URL")
		}
		if r.Header.Get("X-Extra") != "1" {
			t.Error("missing signed header")
		}
		got, _ = io.ReadAll(r.Body)
	})
	up := &UploadURL{URL: c.baseURL.String() + "/upload", Headers: map[string]string{"X-Extra": "1"}}
	if err := c.UploadArtifact(context.Background(), up, bytes.NewReader([]byte("data")), 4); err != nil {
		t.Fatal(err)
	}
	if string(got) != "data" {
		t.Fatalf("uploaded %q", got)
	}
}

func TestDownload(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("API token must not be sent to the download URL")
		}
		_, _ = io.WriteString(w, "0123456789")
	})
	url := c.baseURL.String() + "/starter"

	var buf bytes.Buffer
	if n, err := c.Download(context.Background(), url, &buf, 10); err != nil || n != 10 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if _, err := c.Download(context.Background(), url, io.Discard, 5); !errors.Is(err, ErrDownloadTooLarge) {
		t.Fatalf("err = %v, want ErrDownloadTooLarge", err)
	}
}

// writeData writes v in the API's response envelope.
func writeData(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"status_code": status, "message": "ok", "data": v})
}
