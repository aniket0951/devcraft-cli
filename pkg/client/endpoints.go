package client

// API paths used by the client.
//
// PLACEHOLDERS: these follow the proposed contract in the integration plan
// (§7 "Suggested API endpoints") and will be replaced once the backend API is
// finalised. Keep every path in this file so the swap is a one-file change.
const (
	pathAssignments     = "/v1/assignments"
	pathAssignment      = "/v1/assignments/%s"
	pathAttempts        = "/v1/attempts"
	pathAttempt         = "/v1/attempts/%s"
	pathUploadURL       = "/v1/artifacts/upload-url"
	pathQuickEvaluation = "/v1/evaluations/quick"
	pathSubmissions     = "/v1/attempts/%s/submissions"
	pathSubmission      = "/v1/submissions/%s"
	pathEvaluation      = "/v1/evaluations/%s"
)
