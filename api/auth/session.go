// Package auth is the shared OIDC relying-party building block for must*
// services: session cookies, the Authorization Code + PKCE login flow, and
// the request-context plumbing to read the logged-in identity back out.
//
// Deliberately stateless -- no server-side session store, no DB row per
// login -- so a service stays a plain N-replica-behind-a-load-balancer
// process per the workspace's cloud-native tenet. Every must* service is its
// own OIDC client against whatever external IdP the operator runs; nothing
// here bundles or assumes a particular one.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

var (
	// ErrInvalidSession covers a missing, malformed, or tampered cookie.
	ErrInvalidSession = errors.New("auth: invalid session")
	// ErrSessionExpired means the signature was valid but the session's TTL passed.
	ErrSessionExpired = errors.New("auth: session expired")
)

// Session is the identity carried in the signed session cookie.
type Session struct {
	Subject   string    `json:"sub"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"exp"`
}

// CookieConfig controls how session (and login-flow state) cookies are
// signed and verified. SigningKey is required; everything else has a
// sensible default.
type CookieConfig struct {
	Name       string // cookie name, defaults to "must_session"
	SigningKey []byte // HMAC-SHA256 key
	Domain     string // optional: shares the cookie across a parent domain for SSO
	Secure     bool
}

func (c CookieConfig) cookieName() string {
	if c.Name == "" {
		return "must_session"
	}
	return c.Name
}

// signValue HMAC-signs an arbitrary JSON-able payload as "<payload>.<sig>",
// both base64url. Used for both the session cookie (Session) and the
// short-lived login-flow state cookie (authState) in oidc.go.
func (c CookieConfig) signValue(v any) (string, error) {
	return sign(c.SigningKey, v)
}

// parseValue verifies and decodes a value produced by signValue into v.
func (c CookieConfig) parseValue(value string, v any) error {
	return verify(c.SigningKey, value, v)
}

// sign HMAC-SHA256-signs the JSON encoding of v with key as
// "<payload>.<sig>", both base64url.
func sign(key []byte, v any) (string, error) {
	payload, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	p := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(p))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return p + "." + sig, nil
}

// verify checks a value produced by sign with key and decodes its payload
// into v. Any malformed or tampered value is ErrInvalidSession.
func verify(key []byte, value string, v any) error {
	i := strings.LastIndex(value, ".")
	if i < 0 {
		return ErrInvalidSession
	}
	p, sig := value[:i], value[i+1:]
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(p))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(sig), []byte(want)) != 1 {
		return ErrInvalidSession
	}
	payload, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return ErrInvalidSession
	}
	if err := json.Unmarshal(payload, v); err != nil {
		return ErrInvalidSession
	}
	return nil
}

// SetCookie signs sess and attaches it to the response.
func (c CookieConfig) SetCookie(w http.ResponseWriter, sess Session) error {
	v, err := c.signValue(sess)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     c.cookieName(),
		Value:    v,
		Path:     "/",
		Domain:   c.Domain,
		Expires:  sess.ExpiresAt,
		HttpOnly: true,
		Secure:   c.Secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// ClearCookie removes the session cookie (logout).
func (c CookieConfig) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     c.cookieName(),
		Value:    "",
		Path:     "/",
		Domain:   c.Domain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   c.Secure,
	})
}

// SessionFromRequest reads and verifies the session cookie on r, if any.
func (c CookieConfig) SessionFromRequest(r *http.Request) (Session, error) {
	ck, err := r.Cookie(c.cookieName())
	if err != nil {
		return Session{}, ErrInvalidSession
	}
	var sess Session
	if err := c.parseValue(ck.Value, &sess); err != nil {
		return Session{}, err
	}
	if time.Now().After(sess.ExpiresAt) {
		return Session{}, ErrSessionExpired
	}
	return sess, nil
}
