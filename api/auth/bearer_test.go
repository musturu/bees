package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testTokenConfig() TokenConfig {
	return TokenConfig{SigningKey: []byte("test-signing-key-0123456789abcdef"), AccessTTL: time.Minute}
}

func TestAccessTokenRoundTrip(t *testing.T) {
	cfg := testTokenConfig()
	tok, exp, err := cfg.IssueAccessToken(Session{Subject: "u1", Email: "a@example.test", Role: "ADMIN"})
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}
	if d := time.Until(exp); d <= 0 || d > time.Minute {
		t.Fatalf("expiresAt %v not within AccessTTL of now", exp)
	}

	sess, err := cfg.VerifyAccessToken(tok)
	if err != nil {
		t.Fatalf("VerifyAccessToken: %v", err)
	}
	if sess.Subject != "u1" || sess.Email != "a@example.test" || sess.Role != "ADMIN" {
		t.Fatalf("session = %+v, want the issued identity", sess)
	}
}

func TestIssueAccessTokenRequiresKeyAndTTL(t *testing.T) {
	tests := []struct {
		name string
		cfg  TokenConfig
	}{
		{"no key", TokenConfig{AccessTTL: time.Minute}},
		{"no ttl", TokenConfig{SigningKey: []byte("k")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := tt.cfg.IssueAccessToken(Session{Subject: "u1"}); err == nil {
				t.Fatal("IssueAccessToken succeeded, want an error")
			}
		})
	}
}

func TestVerifyAccessTokenRejects(t *testing.T) {
	cfg := testTokenConfig()
	valid, _, err := cfg.IssueAccessToken(Session{Subject: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	expired, err := sign(cfg.key(), Session{Subject: "u1", ExpiresAt: time.Now().Add(-time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	cookieValue, err := CookieConfig{SigningKey: cfg.SigningKey}.signValue(Session{Subject: "u1", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	otherKey := TokenConfig{SigningKey: []byte("another-key"), AccessTTL: time.Minute}

	tests := []struct {
		name    string
		cfg     TokenConfig
		token   string
		wantErr error
	}{
		{"empty", cfg, "", ErrInvalidSession},
		{"garbage", cfg, "not-a-token", ErrInvalidSession},
		{"tampered", cfg, valid + "x", ErrInvalidSession},
		{"other key", otherKey, valid, ErrInvalidSession},
		{"session cookie with same key", cfg, cookieValue, ErrInvalidSession},
		{"expired", cfg, expired, ErrSessionExpired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.cfg.VerifyAccessToken(tt.token)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRequireBearer(t *testing.T) {
	cfg := testTokenConfig()
	tok, _, err := cfg.IssueAccessToken(Session{Subject: "u1", Role: "VOLUNTEER"})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		header     string
		wantStatus int
	}{
		{"valid", "Bearer " + tok, http.StatusOK},
		{"scheme is case-insensitive", "bearer " + tok, http.StatusOK},
		{"missing header", "", http.StatusUnauthorized},
		{"wrong scheme", "Basic " + tok, http.StatusUnauthorized},
		{"empty token", "Bearer ", http.StatusUnauthorized},
		{"invalid token", "Bearer nope", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := RequireBearer(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sess, ok := SessionFromContext(r.Context())
				if !ok || sess.Subject != "u1" {
					t.Errorf("session in context = %+v, %v; want subject u1", sess, ok)
				}
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatalf("WWW-Authenticate = %q, want %q", rec.Header().Get("WWW-Authenticate"), "Bearer")
			}
		})
	}
}

func TestRequireBearerUsesDeny(t *testing.T) {
	cfg := testTokenConfig()
	cfg.Deny = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h := RequireBearer(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called without a valid token")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want Deny's %d", rec.Code, http.StatusTeapot)
	}
}
