package archive

import "regexp"

// Finding is a likely secret found in a file that would be uploaded.
type Finding struct {
	Path   string
	Reason string
}

var secretPatterns = []struct {
	reason string
	re     *regexp.Regexp
}{
	{"private key", regexp.MustCompile(`-----BEGIN (?:[A-Z]+ )?PRIVATE KEY-----`)},
	{"AWS access key ID", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"GitHub token", regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36}\b`)},
	{"GitHub fine-grained token", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{60,}\b`)},
	{"Slack token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`)},
	{"Stripe live secret key", regexp.MustCompile(`\b[sr]k_live_[0-9A-Za-z]{20,}\b`)},
	{"Google service account key", regexp.MustCompile(`"type"\s*:\s*"service_account"`)},
}

// scanSecrets returns the reasons content looks like it contains secrets.
func scanSecrets(content []byte) []string {
	var reasons []string
	for _, p := range secretPatterns {
		if p.re.Match(content) {
			reasons = append(reasons, p.reason)
		}
	}
	return reasons
}
