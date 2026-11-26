package auth

import (
	"encoding/base64"
	"fmt"
	"testing"
	"time"
)

// jwtWithExp builds an unsigned JWT-shaped token with the given exp claim.
func jwtWithExp(exp int64) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc([]byte(fmt.Sprintf(`{"exp":%d,"email":"a@b.dev"}`, exp))) + ".sig"
}

func TestSessionExpired(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for name, tc := range map[string]struct {
		token string
		want  bool
	}{
		"future":       {jwtWithExp(now.Add(time.Hour).Unix()), false},
		"past":         {jwtWithExp(now.Add(-time.Minute).Unix()), true},
		"within skew":  {jwtWithExp(now.Add(10 * time.Second).Unix()), true},
		"not a JWT":    {"opaque-token", false},
		"bad payload":  {"a.!!!.c", false},
		"no exp claim": {"e30.e30.sig", false}, // {}.{}
		"empty":        {"", false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := (Session{IDToken: tc.token}).Expired(now); got != tc.want {
				t.Fatalf("Expired = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSessionExpiresAt(t *testing.T) {
	exp, ok := Session{IDToken: jwtWithExp(1_800_000_000)}.ExpiresAt()
	if !ok || exp.Unix() != 1_800_000_000 {
		t.Fatalf("exp=%v ok=%v", exp, ok)
	}
}
