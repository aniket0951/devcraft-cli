package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ErrSessionExpired is returned when the stored login can no longer be used:
// its ID token has expired, or the API rejected it.
var ErrSessionExpired = errors.New("session expired")

// expirySkew treats a token as expired slightly early so it doesn't lapse
// mid-request.
const expirySkew = 30 * time.Second

// ExpiresAt returns the "exp" claim of the session's ID token (a JWT). The
// signature is not checked: this is only to decide whether to sign in again
// before calling the API, which verifies the token itself. ok is false when
// the token isn't a JWT with an exp claim.
func (s Session) ExpiresAt() (exp time.Time, ok bool) {
	parts := strings.Split(s.IDToken, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp <= 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Exp, 0), true
}

// Expired reports whether the ID token has expired (or will within a few
// seconds) at now. Tokens without a readable expiry are left to the API.
func (s Session) Expired(now time.Time) bool {
	exp, ok := s.ExpiresAt()
	return ok && !now.Add(expirySkew).Before(exp)
}
