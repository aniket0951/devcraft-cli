// Package client is an HTTP client for the DevCraft REST API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 10 << 20

// Client talks to the DevCraft API on behalf of one authenticated user.
type Client struct {
	baseURL    *url.URL
	token      string
	httpClient *http.Client
	userAgent  string
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sets the underlying HTTP client.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// WithUserAgent sets the User-Agent header sent with every request.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

// New returns a client for the API at baseURL that authenticates with token.
func New(baseURL, token string, opts ...Option) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid API URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("invalid API URL %q: scheme must be http or https", baseURL)
	}
	c := &Client{
		baseURL:    u,
		token:      token,
		httpClient: &http.Client{Timeout: 60 * time.Second},
		userAgent:  "devcraft-cli",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// APIError is a non-2xx response from the API.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	if e.Code != "" {
		return fmt.Sprintf("api error %d (%s): %s", e.StatusCode, e.Code, msg)
	}
	return fmt.Sprintf("api error %d: %s", e.StatusCode, msg)
}

// IsUnauthorized reports whether err is a 401 from the API.
func IsUnauthorized(err error) bool { return hasStatus(err, http.StatusUnauthorized) }

// IsNotFound reports whether err is a 404 from the API.
func IsNotFound(err error) bool { return hasStatus(err, http.StatusNotFound) }

func hasStatus(err error, code int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == code
}

// ListAssignments returns the published assignments matching p, by category
// then code.
func (c *Client) ListAssignments(ctx context.Context, p ListAssignmentsParams) ([]Assignment, error) {
	q := url.Values{}
	setIf(q, "language", p.Language)
	setIf(q, "category", p.Category)
	setIf(q, "level", p.Level)
	var out []Assignment
	return out, c.do(ctx, http.MethodGet, pathAssignments, q, nil, &out)
}

// GetAssignment returns one assignment.
func (c *Client) GetAssignment(ctx context.Context, id string) (*Assignment, error) {
	var out Assignment
	return &out, c.do(ctx, http.MethodGet, pathf(pathAssignment, id), nil, nil, &out)
}

// GetAssignmentDownload returns a short-lived download URL for the current
// version of a published assignment, with the size and SHA-256 the
// downloaded ZIP must match.
func (c *Client) GetAssignmentDownload(ctx context.Context, code string) (*AssignmentDownload, error) {
	var out AssignmentDownload
	return &out, c.do(ctx, http.MethodGet, pathf(pathAssignmentDownload, code), nil, nil, &out)
}

// CreateAttempt starts an attempt on an assignment. The response carries a
// short-lived starter artifact URL.
func (c *Client) CreateAttempt(ctx context.Context, assignmentID string) (*Attempt, error) {
	body := map[string]string{"assignment_id": assignmentID}
	var out Attempt
	return &out, c.do(ctx, http.MethodPost, pathAttempts, nil, body, &out)
}

// GetAttempt returns one attempt.
func (c *Client) GetAttempt(ctx context.Context, id string) (*Attempt, error) {
	var out Attempt
	return &out, c.do(ctx, http.MethodGet, pathf(pathAttempt, id), nil, nil, &out)
}

// RequestUploadURL asks for a short-lived URL to upload an artifact to.
func (c *Client) RequestUploadURL(ctx context.Context, req UploadURLRequest) (*UploadURL, error) {
	var out UploadURL
	return &out, c.do(ctx, http.MethodPost, pathUploadURL, nil, req, &out)
}

// CreateQuickEvaluation requests a quick (preview) evaluation of an artifact.
func (c *Client) CreateQuickEvaluation(ctx context.Context, attemptID, artifactID string) (*Evaluation, error) {
	body := map[string]string{"attempt_id": attemptID, "artifact_id": artifactID}
	var out Evaluation
	return &out, c.do(ctx, http.MethodPost, pathQuickEvaluation, nil, body, &out)
}

// CreateSubmission creates a formal submission from an uploaded artifact.
func (c *Client) CreateSubmission(ctx context.Context, attemptID, artifactID, checksum string) (*Submission, error) {
	body := map[string]string{"artifact_id": artifactID, "checksum": checksum}
	var out Submission
	return &out, c.do(ctx, http.MethodPost, pathf(pathSubmissions, attemptID), nil, body, &out)
}

// GetSubmission returns one submission.
func (c *Client) GetSubmission(ctx context.Context, id string) (*Submission, error) {
	var out Submission
	return &out, c.do(ctx, http.MethodGet, pathf(pathSubmission, id), nil, nil, &out)
}

// GetEvaluation returns one evaluation.
func (c *Client) GetEvaluation(ctx context.Context, id string) (*Evaluation, error) {
	var out Evaluation
	return &out, c.do(ctx, http.MethodGet, pathf(pathEvaluation, id), nil, nil, &out)
}

// UploadArtifact uploads size bytes from body to a pre-signed upload URL.
// The API token is never sent to the upload URL.
func (c *Client) UploadArtifact(ctx context.Context, u *UploadURL, body io.Reader, size int64) error {
	method := u.Method
	if method == "" {
		method = http.MethodPut
	}
	req, err := http.NewRequestWithContext(ctx, method, u.URL, body)
	if err != nil {
		return fmt.Errorf("build upload request: %w", err)
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/gzip")
	for k, v := range u.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload artifact: %w", stripURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &APIError{StatusCode: resp.StatusCode, Message: "artifact upload failed"}
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
	return nil
}

// ErrDownloadTooLarge is returned when a download exceeds its size limit.
var ErrDownloadTooLarge = errors.New("download exceeds size limit")

// Download fetches a pre-signed URL into w, failing if it exceeds maxBytes.
// The API token is never sent to the download URL.
func (c *Client) Download(ctx context.Context, rawURL string, w io.Writer, maxBytes int64) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("download: %w", stripURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, &APIError{StatusCode: resp.StatusCode, Message: "download failed"}
	}
	if resp.ContentLength > maxBytes {
		return 0, ErrDownloadTooLarge
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return n, fmt.Errorf("download: %w", stripURL(err))
	}
	if n > maxBytes {
		return n, ErrDownloadTooLarge
	}
	return n, nil
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	// path is already escaped (see pathf), so join it as a raw string.
	u, err := url.Parse(c.baseURL.String() + path)
	if err != nil {
		return fmt.Errorf("build URL: %w", err)
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reqBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return parseAPIError(resp.StatusCode, data)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	// Every API response is an envelope: {"status_code", "message", "data", "error"}.
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("decode response from %s: %w", path, err)
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return fmt.Errorf("decode response from %s: no data in response", path)
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("decode response from %s: %w", path, err)
	}
	return nil
}

// parseAPIError reads the error envelope
// {"message": "...", "error": {"err_code": "...", "err_msg": "..."}},
// falling back to the top-level message and then the status.
func parseAPIError(status int, data []byte) error {
	var body struct {
		Message string `json:"message"`
		Error   struct {
			Code    string `json:"err_code"`
			Message string `json:"err_msg"`
		} `json:"error"`
	}
	apiErr := &APIError{StatusCode: status}
	if json.Unmarshal(data, &body) == nil {
		apiErr.Code = body.Error.Code
		apiErr.Message = body.Error.Message
		if apiErr.Message == "" {
			apiErr.Message = body.Message
		}
	}
	return apiErr
}

// stripURL drops the URL from a *url.Error so a pre-signed URL (a bearer
// capability) never ends up in an error message.
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

func pathf(format, id string) string {
	return fmt.Sprintf(format, url.PathEscape(id))
}

func setIf(q url.Values, key, val string) {
	if val != "" {
		q.Set(key, val)
	}
}
