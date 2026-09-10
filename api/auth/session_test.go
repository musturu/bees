package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testCookieConfig() CookieConfig {
	return CookieConfig{SigningKey: []byte("test-signing-key")}
}

func TestSessionRoundTrip(t *testing.T) {
	cfg := testCookieConfig()
	want := Session{Subject: "user-1", Email: "a@example.com", Role: "member", ExpiresAt: time.Now().Add(time.Hour)}

	rec := httptest.NewRecorder()
	if err := cfg.SetCookie(rec, want); err != nil {
		t.Fatalf("SetCookie: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}

	got, err := cfg.SessionFromRequest(req)
	if err != nil {
		t.Fatalf("SessionFromRequest: %v", err)
	}
	if got.Subject != want.Subject || got.Email != want.Email || got.Role != want.Role {
		t.Fatalf("SessionFromRequest: got %+v, want %+v", got, want)
	}
}

func TestSessionFromRequestTampered(t *testing.T) {
	cfg := testCookieConfig()
	sess := Session{Subject: "user-1", Role: "member", ExpiresAt: time.Now().Add(time.Hour)}

	rec := httptest.NewRecorder()
	if err := cfg.SetCookie(rec, sess); err != nil {
		t.Fatalf("SetCookie: %v", err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	cookies[0].Value += "x" // corrupt the signature

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookies[0])

	if _, err := cfg.SessionFromRequest(req); err != ErrInvalidSession {
		t.Fatalf("SessionFromRequest(tampered): got err %v, want ErrInvalidSession", err)
	}
}

func TestSessionFromRequestWrongKey(t *testing.T) {
	sess := Session{Subject: "user-1", Role: "member", ExpiresAt: time.Now().Add(time.Hour)}

	rec := httptest.NewRecorder()
	if err := testCookieConfig().SetCookie(rec, sess); err != nil {
		t.Fatalf("SetCookie: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}

	other := CookieConfig{SigningKey: []byte("a-different-key")}
	if _, err := other.SessionFromRequest(req); err != ErrInvalidSession {
		t.Fatalf("SessionFromRequest(wrong key): got err %v, want ErrInvalidSession", err)
	}
}

func TestSessionFromRequestExpired(t *testing.T) {
	cfg := testCookieConfig()
	sess := Session{Subject: "user-1", Role: "member", ExpiresAt: time.Now().Add(-time.Minute)}

	rec := httptest.NewRecorder()
	if err := cfg.SetCookie(rec, sess); err != nil {
		t.Fatalf("SetCookie: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}

	if _, err := cfg.SessionFromRequest(req); err != ErrSessionExpired {
		t.Fatalf("SessionFromRequest(expired): got err %v, want ErrSessionExpired", err)
	}
}

func TestSessionFromRequestMissing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, err := testCookieConfig().SessionFromRequest(req); err != ErrInvalidSession {
		t.Fatalf("SessionFromRequest(no cookie): got err %v, want ErrInvalidSession", err)
	}
}

func TestClearCookie(t *testing.T) {
	cfg := testCookieConfig()
	rec := httptest.NewRecorder()
	cfg.ClearCookie(rec)
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Fatalf("ClearCookie: expected one cookie with negative MaxAge, got %+v", cookies)
	}
}
