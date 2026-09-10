package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Config configures an OIDC relying party against an external IdP -- the
// operator's own Keycloak, Dex, Okta, etc. Nothing in this package stands
// one up or assumes which one is in use.
type Config struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// Scopes defaults to {openid, email, profile}.
	Scopes []string
	// GroupsClaim names the ID token claim holding the caller's group/role
	// list. Defaults to "groups".
	GroupsClaim string
	// AdminGroup is the group name that maps to Session.Role "admin";
	// everyone else authenticated gets "member". Leaving it empty means
	// every logged-in identity is a "member" -- no admin claim to check.
	AdminGroup string
	Cookie     CookieConfig
	// SessionTTL defaults to 12h.
	SessionTTL time.Duration
}

// Provider is a ready-to-mount OIDC relying party: LoginHandler,
// CallbackHandler and LogoutHandler are plain http.HandlerFuncs a service
// registers under e.g. /auth/login, /auth/callback, /auth/logout.
type Provider struct {
	cfg      Config
	verifier *oidc.IDTokenVerifier
	oauth2   oauth2.Config
}

// NewProvider runs OIDC discovery against cfg.IssuerURL and returns a
// ready-to-use Provider.
func NewProvider(ctx context.Context, cfg Config) (*Provider, error) {
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{oidc.ScopeOpenID, "email", "profile"}
	}
	if cfg.GroupsClaim == "" {
		cfg.GroupsClaim = "groups"
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 12 * time.Hour
	}
	p, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("auth: oidc discovery against %q: %w", cfg.IssuerURL, err)
	}
	return &Provider{
		cfg:      cfg,
		verifier: p.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth2: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     p.Endpoint(),
			Scopes:       cfg.Scopes,
		},
	}, nil
}

// authState is the short-lived, signed cookie value carrying PKCE + CSRF
// state across the redirect to the IdP and back. Not server-side session
// storage -- it rides in the browser, same as the eventual session cookie.
type authState struct {
	State    string    `json:"state"`
	Verifier string    `json:"verifier"`
	ReturnTo string    `json:"return_to"`
	Expires  time.Time `json:"exp"`
}

const stateCookieName = "must_oauth_state"

// LoginHandler starts the Authorization Code + PKCE flow and redirects to
// the IdP. An optional ?return_to=/some/path is honored after login.
func (p *Provider) LoginHandler(w http.ResponseWriter, r *http.Request) {
	verifier := oauth2.GenerateVerifier()
	state := randState()
	st := authState{
		State:    state,
		Verifier: verifier,
		ReturnTo: sanitizeReturnTo(r.URL.Query().Get("return_to")),
		Expires:  time.Now().Add(10 * time.Minute),
	}
	v, err := p.cfg.Cookie.signValue(st)
	if err != nil {
		http.Error(w, "auth: internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    v,
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   p.cfg.Cookie.Secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, p.oauth2.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), http.StatusSeeOther)
}

// CallbackHandler completes the flow: exchanges the code, verifies the ID
// token, and mints a session cookie in its place.
func (p *Provider) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	ck, err := r.Cookie(stateCookieName)
	if err != nil {
		http.Error(w, "auth: missing or expired login attempt, please try again", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: stateCookieName, Value: "", Path: "/", MaxAge: -1})

	var st authState
	if err := p.cfg.Cookie.parseValue(ck.Value, &st); err != nil || time.Now().After(st.Expires) {
		http.Error(w, "auth: invalid or expired login attempt, please try again", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("state") != st.State {
		http.Error(w, "auth: state mismatch", http.StatusBadRequest)
		return
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		http.Error(w, "auth: login failed: "+errParam, http.StatusUnauthorized)
		return
	}

	tok, err := p.oauth2.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(st.Verifier))
	if err != nil {
		http.Error(w, "auth: token exchange failed", http.StatusBadGateway)
		return
	}
	rawIDToken, ok := tok.Extra("id_token").(string)
	if !ok {
		http.Error(w, "auth: identity provider did not return an id_token", http.StatusBadGateway)
		return
	}
	idTok, err := p.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Error(w, "auth: id token verification failed", http.StatusUnauthorized)
		return
	}

	var claims struct {
		Email string `json:"email"`
	}
	var rawClaims map[string]any
	if err := idTok.Claims(&claims); err != nil {
		http.Error(w, "auth: invalid id token claims", http.StatusUnauthorized)
		return
	}
	if err := idTok.Claims(&rawClaims); err != nil {
		http.Error(w, "auth: invalid id token claims", http.StatusUnauthorized)
		return
	}

	sess := Session{
		Subject:   idTok.Subject,
		Email:     claims.Email,
		Role:      roleFromClaims(rawClaims, p.cfg.GroupsClaim, p.cfg.AdminGroup),
		ExpiresAt: time.Now().Add(p.cfg.SessionTTL),
	}
	if err := p.cfg.Cookie.SetCookie(w, sess); err != nil {
		http.Error(w, "auth: internal error", http.StatusInternalServerError)
		return
	}
	returnTo := st.ReturnTo
	if returnTo == "" {
		returnTo = "/"
	}
	http.Redirect(w, r, returnTo, http.StatusSeeOther)
}

// LogoutHandler clears the session cookie. It does not attempt IdP-side
// single-logout -- that's an operator/IdP-specific concern out of scope
// here.
func (p *Provider) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	p.cfg.Cookie.ClearCookie(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// roleFromClaims maps an ID token's group list to a Session.Role: "admin"
// if adminGroup is present among groupsClaim's values, "member" otherwise
// (including when adminGroup is unset, or the claim is missing/malformed).
func roleFromClaims(claims map[string]any, groupsClaim, adminGroup string) string {
	if adminGroup == "" {
		return "member"
	}
	raw, ok := claims[groupsClaim]
	if !ok {
		return "member"
	}
	groups, ok := raw.([]any)
	if !ok {
		return "member"
	}
	for _, g := range groups {
		if s, ok := g.(string); ok && s == adminGroup {
			return "admin"
		}
	}
	return "member"
}

// sanitizeReturnTo only allows a same-site relative path, to rule out an
// open redirect via a crafted ?return_to=.
func sanitizeReturnTo(v string) string {
	if v == "" || !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") {
		return ""
	}
	return v
}

func randState() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read only fails if the OS entropy source is broken,
		// a condition nothing in this handler could recover from anyway.
		panic("auth: crypto/rand unavailable: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
