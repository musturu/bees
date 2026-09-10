package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	josepkg "github.com/go-jose/go-jose/v4"
)

func TestRoleFromClaims(t *testing.T) {
	cases := []struct {
		name       string
		claims     map[string]any
		groupsKey  string
		adminGroup string
		want       string
	}{
		{"no admin group configured", map[string]any{"groups": []any{"admins"}}, "groups", "", "member"},
		{"member not in admin group", map[string]any{"groups": []any{"devs"}}, "groups", "admins", "member"},
		{"in admin group", map[string]any{"groups": []any{"devs", "admins"}}, "groups", "admins", "admin"},
		{"missing claim", map[string]any{}, "groups", "admins", "member"},
		{"claim wrong type", map[string]any{"groups": "admins"}, "groups", "admins", "member"},
		{"custom claim name", map[string]any{"roles": []any{"admins"}}, "roles", "admins", "admin"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := roleFromClaims(c.claims, c.groupsKey, c.adminGroup); got != c.want {
				t.Errorf("roleFromClaims(%v, %q, %q) = %q, want %q", c.claims, c.groupsKey, c.adminGroup, got, c.want)
			}
		})
	}
}

func TestSanitizeReturnTo(t *testing.T) {
	cases := map[string]string{
		"":                         "",
		"/repo/hello":              "/repo/hello",
		"//evil.example.com/x":     "",
		"https://evil.example.com": "",
		"relative/path":            "",
	}
	for in, want := range cases {
		if got := sanitizeReturnTo(in); got != want {
			t.Errorf("sanitizeReturnTo(%q) = %q, want %q", in, got, want)
		}
	}
}

// newFakeIdP starts a minimal OIDC provider: a discovery document, a JWKS
// endpoint, and a token endpoint that always returns idToken (pre-signed
// before the server starts, so the handler needs no access to *testing.T --
// it runs on the httptest server's own goroutine).
func newFakeIdP(t *testing.T, clientID, subject string, extraClaims map[string]any) (issuerURL string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	const kid = "test-key"

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	idToken := signTestIDToken(t, key, kid, srv.URL, clientID, subject, extraClaims)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                srv.URL,
			"authorization_endpoint":                srv.URL + "/authorize",
			"token_endpoint":                        srv.URL + "/token",
			"jwks_uri":                              srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		jwk := josepkg.JSONWebKey{Key: &key.PublicKey, KeyID: kid, Algorithm: "RS256", Use: "sig"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(josepkg.JSONWebKeySet{Keys: []josepkg.JSONWebKey{jwk}})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {}) // never hit directly; only Location is inspected
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fake-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"id_token":     idToken,
		})
	})

	return srv.URL
}

func signTestIDToken(t *testing.T, key *rsa.PrivateKey, kid, issuer, audience, subject string, extraClaims map[string]any) string {
	t.Helper()
	signingKey := josepkg.SigningKey{
		Algorithm: josepkg.RS256,
		Key:       josepkg.JSONWebKey{Key: key, KeyID: kid, Algorithm: "RS256", Use: "sig"},
	}
	signer, err := josepkg.NewSigner(signingKey, &josepkg.SignerOptions{})
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}

	claims := map[string]any{
		"iss": issuer,
		"sub": subject,
		"aud": audience,
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	for k, v := range extraClaims {
		claims[k] = v
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	jws, err := signer.Sign(payload)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	compact, err := jws.CompactSerialize()
	if err != nil {
		t.Fatalf("CompactSerialize: %v", err)
	}
	return compact
}

func TestProviderLoginAndCallback(t *testing.T) {
	issuerURL := newFakeIdP(t, "must-client", "user-123", map[string]any{
		"email":  "person@example.com",
		"groups": []any{"admins"},
	})

	cfg := Config{
		IssuerURL:    issuerURL,
		ClientID:     "must-client",
		ClientSecret: "secret",
		RedirectURL:  "https://mustgit.example.com/auth/callback",
		AdminGroup:   "admins",
		Cookie:       CookieConfig{SigningKey: []byte("test-signing-key")},
	}
	p, err := NewProvider(t.Context(), cfg)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	// 1. LoginHandler: capture the state cookie + the state embedded in the
	// (unfollowed) redirect to the IdP's authorize endpoint.
	loginReq := httptest.NewRequest(http.MethodGet, "/auth/login?return_to=/hello", nil)
	loginRec := httptest.NewRecorder()
	p.LoginHandler(loginRec, loginReq)
	if loginRec.Code != http.StatusSeeOther {
		t.Fatalf("LoginHandler: status = %d, want %d", loginRec.Code, http.StatusSeeOther)
	}
	loc, err := url.Parse(loginRec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatalf("LoginHandler: redirect missing state param: %s", loc)
	}
	var stateCookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == stateCookieName {
			stateCookie = c
		}
	}
	if stateCookie == nil {
		t.Fatalf("LoginHandler: did not set %s cookie", stateCookieName)
	}

	// 2. CallbackHandler: simulate the IdP redirecting back with ?code&state,
	// carrying the state cookie set in step 1.
	cbReq := httptest.NewRequest(http.MethodGet, "/auth/callback?code=fake-code&state="+state, nil)
	cbReq.AddCookie(stateCookie)
	cbRec := httptest.NewRecorder()
	p.CallbackHandler(cbRec, cbReq)

	if cbRec.Code != http.StatusSeeOther {
		t.Fatalf("CallbackHandler: status = %d, body = %s", cbRec.Code, cbRec.Body.String())
	}
	if got := cbRec.Header().Get("Location"); got != "/hello" {
		t.Fatalf("CallbackHandler: redirected to %q, want %q", got, "/hello")
	}

	var sessionCookie *http.Cookie
	for _, c := range cbRec.Result().Cookies() {
		if c.Name == "must_session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatalf("CallbackHandler: did not set a session cookie")
	}

	verifyReq := httptest.NewRequest(http.MethodGet, "/", nil)
	verifyReq.AddCookie(sessionCookie)
	sess, err := cfg.Cookie.SessionFromRequest(verifyReq)
	if err != nil {
		t.Fatalf("SessionFromRequest: %v", err)
	}
	if sess.Subject != "user-123" || sess.Email != "person@example.com" || sess.Role != "admin" {
		t.Fatalf("session = %+v, want subject=user-123 email=person@example.com role=admin", sess)
	}
}
