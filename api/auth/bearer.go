package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
	"time"

	"bees/api/runtime/http/middleware"
)

// accessTokenKeyLabel derives the access-token signing key from
// TokenConfig.SigningKey, so a session cookie signed with the same key is
// never accepted as a bearer token (and vice versa).
const accessTokenKeyLabel = "bees/auth: bearer access token"

// TokenConfig controls how bearer access tokens are signed and verified.
//
// Like CookieConfig it is stateless: an access token is a short-lived,
// signed Session that can be verified without a lookup. Refresh tokens,
// and their persistence, rotation and revocation, belong to the consuming
// service.
type TokenConfig struct {
	SigningKey []byte        // HMAC-SHA256 key
	AccessTTL  time.Duration // lifetime of an issued access token

	// Deny writes the response for a request without a valid bearer
	// token, e.g. to render the service's own error format. The
	// WWW-Authenticate header is already set when it runs. If nil,
	// RequireBearer answers with a bare 401.
	Deny http.Handler
}

func (c TokenConfig) key() []byte {
	mac := hmac.New(sha256.New, c.SigningKey)
	mac.Write([]byte(accessTokenKeyLabel))
	return mac.Sum(nil)
}

// IssueAccessToken signs sess into an opaque bearer token that expires
// AccessTTL from now. sess.ExpiresAt is overwritten with that expiry,
// which is also returned.
func (c TokenConfig) IssueAccessToken(sess Session) (string, time.Time, error) {
	if len(c.SigningKey) == 0 {
		return "", time.Time{}, errors.New("auth: token signing key is required")
	}
	if c.AccessTTL <= 0 {
		return "", time.Time{}, errors.New("auth: access token TTL must be positive")
	}
	sess.ExpiresAt = time.Now().Add(c.AccessTTL)
	token, err := sign(c.key(), sess)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, sess.ExpiresAt, nil
}

// VerifyAccessToken verifies a token produced by IssueAccessToken and
// returns the Session it carries. It returns ErrInvalidSession for a
// malformed or tampered token and ErrSessionExpired for an expired one.
func (c TokenConfig) VerifyAccessToken(token string) (Session, error) {
	if len(c.SigningKey) == 0 {
		return Session{}, ErrInvalidSession
	}
	var sess Session
	if err := verify(c.key(), token, &sess); err != nil {
		return Session{}, err
	}
	if !time.Now().Before(sess.ExpiresAt) {
		return Session{}, ErrSessionExpired
	}
	return sess, nil
}

// RequireBearer returns middleware that rejects requests without a valid
// "Authorization: Bearer <token>" header and otherwise stores the token's
// Session in the request context (see SessionFromContext).
//
// Unlike RequireSession it never redirects: a bearer-token caller can't
// follow a redirect into an HTML login page.
func RequireBearer(cfg TokenConfig) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token, ok := bearerToken(r); ok {
				if sess, err := cfg.VerifyAccessToken(token); err == nil {
					next.ServeHTTP(w, r.WithContext(ContextWithSession(r.Context(), sess)))
					return
				}
			}
			w.Header().Set("WWW-Authenticate", "Bearer")
			if cfg.Deny != nil {
				cfg.Deny.ServeHTTP(w, r)
				return
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		})
	}
}

// bearerToken extracts the token from r's Authorization header.
func bearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}
