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
		if q.Get("language") != "go" || q.Get("limit") != "5" || q.Get("cursor") != "c1" || q.Has("category") {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(AssignmentList{
			Items:      []Assignment{{ID: "GO-1", Title: "Go one"}},
			NextCursor: "c2",
		})
	})

	got, err := c.ListAssignments(context.Background(), ListAssignmentsParams{Language: "go", Limit: 5, Cursor: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != "GO-1" || got.NextCursor != "c2" {
		t.Fatalf("got %+v", got)
	}
}

func TestPathEscaping(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/v1/attempts/a%2Fb" {
			t.Errorf("escaped path = %s", r.URL.EscapedPath())
		}
		_ = json.NewEncoder(w).Encode(Attempt{ID: "a/b"})
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
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Submission{ID: "sub_1"})
	})
	sub, err := c.CreateSubmission(context.Background(), "att_1", "art_1", "abc")
	if err != nil || sub.ID != "sub_1" {
		t.Fatalf("sub=%+v err=%v", sub, err)
	}
}

func TestAPIErrorParsing(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"code":"token_expired","message":"session expired"}}`)
	})
	_, err := c.GetEvaluation(context.Background(), "e1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %T %v", err, err)
	}
	if apiErr.Code != "token_expired" || apiErr.Message != "session expired" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if !IsUnauthorized(err) || IsNotFound(err) {
		t.Fatal("status helpers wrong")
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
