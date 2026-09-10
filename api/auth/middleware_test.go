package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequireSessionNoCookieRedirectsBrowserNavigation(t *testing.T) {
	mw := RequireSession(testCookieConfig())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called without a valid session")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc == "" || loc[:len("/auth/login")] != "/auth/login" {
		t.Fatalf("Location = %q, want a redirect to /auth/login", loc)
	}
}

func TestRequireSessionNoCookieRejectsAPIRequest(t *testing.T) {
	mw := RequireSession(testCookieConfig())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called without a valid session")
	}))

	req := httptest.NewRequest(http.MethodPost, "/repos", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireSessionValidCookiePassesThrough(t *testing.T) {
	cfg := testCookieConfig()
	sess := Session{Subject: "user-1", Role: "member", ExpiresAt: time.Now().Add(time.Hour)}

	setRec := httptest.NewRecorder()
	if err := cfg.SetCookie(setRec, sess); err != nil {
		t.Fatalf("SetCookie: %v", err)
	}

	var gotSubject string
	mw := RequireSession(cfg)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := SessionFromContext(r.Context())
		if !ok {
			t.Fatal("SessionFromContext: no session in request context")
		}
		gotSubject = got.Subject
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range setRec.Result().Cookies() {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotSubject != "user-1" {
		t.Fatalf("SessionFromContext: subject = %q, want %q", gotSubject, "user-1")
	}
}
