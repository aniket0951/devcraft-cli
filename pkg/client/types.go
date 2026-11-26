package client

import "time"

// Assignment is a published assignment.
type Assignment struct {
	Code           string `json:"assignment_code"` // e.g. GO-SRP-01; what `devcraft start` takes
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	Language       string `json:"language"`
	CategoryName   string `json:"category_name"`
	Level          string `json:"level"`
	Label          string `json:"label,omitempty"`
	CurrentVersion string `json:"current_version"` // semver, e.g. 1.0.0
}

// ListAssignmentsParams filters the assignment list. Empty fields are
// omitted from the request.
type ListAssignmentsParams struct {
	Language string
	Category string // category name, case-insensitive
	Level    string
}

// AssignmentDownload is a short-lived, pre-signed URL for an assignment's
// starter ZIP. DownloadURL is a bearer capability: never print or log it.
// The downloaded bytes must match SizeBytes and SHA256Hex.
type AssignmentDownload struct {
	AssignmentCode string    `json:"assignment_code"`
	Version        string    `json:"version"` // semver, e.g. 1.0.0
	DownloadURL    string    `json:"download_url"`
	SizeBytes      int64     `json:"size_bytes"`
	SHA256Hex      string    `json:"sha256_hex"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// Attempt is a user's attempt at one assignment version.
type Attempt struct {
	ID                 string    `json:"id"`
	AssignmentID       string    `json:"assignment_id"`
	AssignmentVersion  int       `json:"assignment_version"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"created_at"`
	StarterURL         string    `json:"starter_url,omitempty"`
	StarterChecksum    string    `json:"starter_checksum,omitempty"` // hex SHA-256 of the starter archive
	LatestSubmissionID string    `json:"latest_submission_id,omitempty"`
	LatestEvaluationID string    `json:"latest_evaluation_id,omitempty"`
}

// ArtifactPurpose says what an uploaded artifact will be used for.
type ArtifactPurpose string

const (
	PurposeQuickEvaluation ArtifactPurpose = "quick_evaluation"
	PurposeSubmission      ArtifactPurpose = "submission"
)

// UploadURLRequest asks the API for a short-lived upload URL.
type UploadURLRequest struct {
	AttemptID string          `json:"attempt_id"`
	Purpose   ArtifactPurpose `json:"purpose"`
	Checksum  string          `json:"checksum"` // hex SHA-256 of the archive
	Size      int64           `json:"size"`
}

// UploadURL is a short-lived, pre-signed location to upload an artifact to.
type UploadURL struct {
	ArtifactID string            `json:"artifact_id"`
	URL        string            `json:"url"`
	Method     string            `json:"method,omitempty"` // defaults to PUT
	Headers    map[string]string `json:"headers,omitempty"`
	ExpiresAt  time.Time         `json:"expires_at"`
}

// Submission is a formal, immutable submission of an artifact.
type Submission struct {
	ID           string    `json:"id"`
	AttemptID    string    `json:"attempt_id"`
	ArtifactID   string    `json:"artifact_id"`
	Checksum     string    `json:"checksum"`
	Status       string    `json:"status"`
	EvaluationID string    `json:"evaluation_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// EvaluationStatus is the lifecycle state of an evaluation.
type EvaluationStatus string

const (
	EvaluationQueued     EvaluationStatus = "queued"
	EvaluationRunning    EvaluationStatus = "running"
	EvaluationCompleted  EvaluationStatus = "completed"
	EvaluationInfraError EvaluationStatus = "infra_error"
)

// Check outcomes.
const (
	CheckPassed  = "passed"
	CheckFailed  = "failed"
	CheckSkipped = "skipped"
)

// Evaluation is the result of running the evaluator on an artifact.
type Evaluation struct {
	ID           string           `json:"id"`
	Kind         string           `json:"kind"` // "quick" or "formal"
	Status       EvaluationStatus `json:"status"`
	SubmissionID string           `json:"submission_id,omitempty"`
	Passed       bool             `json:"passed"`
	Score        float64          `json:"score"`
	MaxScore     float64          `json:"max_score"`
	Checks       []Check          `json:"checks,omitempty"`
	Rubric       []RubricScore    `json:"rubric,omitempty"`
	Suggestions  []string         `json:"suggestions,omitempty"`
	InfraError   *InfraError      `json:"infra_error,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	CompletedAt  *time.Time       `json:"completed_at,omitempty"`
}

// Done reports whether the evaluation has reached a terminal state.
func (e *Evaluation) Done() bool {
	return e.Status == EvaluationCompleted || e.Status == EvaluationInfraError
}

// Check is one learner-facing check result.
type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// RubricScore is a score for one rubric criterion.
type RubricScore struct {
	Criterion string  `json:"criterion"`
	Score     float64 `json:"score"`
	MaxScore  float64 `json:"max_score"`
	Evidence  string  `json:"evidence,omitempty"`
}

// InfraError describes an evaluation that failed because of the platform,
// not because of the learner's code.
type InfraError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
