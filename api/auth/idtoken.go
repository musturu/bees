package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/coreos/go-oidc/v3/oidc"
)

// IDTokenVerifier verifies OIDC ID tokens that a native app obtained
// directly from an identity provider (e.g. Sign in with Google or Apple)
// and sends to the service, as opposed to Provider's browser redirect flow.
type IDTokenVerifier struct {
	verifier  *oidc.IDTokenVerifier
	clientIDs []string
}

// IDClaims are the identity claims of a verified ID token.
type IDClaims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Nonce         string
}

// NewIDTokenVerifier returns a verifier for tokens issued by issuer and
// signed with the keys published at jwksURL. A token is accepted only if
// its audience includes one of clientIDs (e.g. an app's iOS, Android and
// web client ids). Keys are fetched lazily, so this makes no network call.
// ctx bounds the background key fetches.
func NewIDTokenVerifier(ctx context.Context, issuer, jwksURL string, clientIDs ...string) *IDTokenVerifier {
	return newIDTokenVerifier(issuer, oidc.NewRemoteKeySet(ctx, jwksURL), clientIDs...)
}

func newIDTokenVerifier(issuer string, keys oidc.KeySet, clientIDs ...string) *IDTokenVerifier {
	return &IDTokenVerifier{
		// The audience is checked in Verify, against any of clientIDs.
		verifier:  oidc.NewVerifier(issuer, keys, &oidc.Config{SkipClientIDCheck: true}),
		clientIDs: clientIDs,
	}
}

// Verify checks raw's signature, issuer, expiry and audience, and returns
// its identity claims.
func (v *IDTokenVerifier) Verify(ctx context.Context, raw string) (IDClaims, error) {
	tok, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return IDClaims{}, fmt.Errorf("auth: verifying id token: %w", err)
	}
	if !slices.ContainsFunc(tok.Audience, func(aud string) bool { return slices.Contains(v.clientIDs, aud) }) {
		return IDClaims{}, errors.New("auth: id token audience matches no client id")
	}
	if tok.Subject == "" {
		return IDClaims{}, errors.New("auth: id token has no subject")
	}

	var c struct {
		Email         string          `json:"email"`
		EmailVerified json.RawMessage `json:"email_verified"`
		Name          string          `json:"name"`
	}
	if err := tok.Claims(&c); err != nil {
		return IDClaims{}, fmt.Errorf("auth: reading id token claims: %w", err)
	}
	return IDClaims{
		Subject:       tok.Subject,
		Email:         c.Email,
		EmailVerified: isTrue(c.EmailVerified),
		Name:          c.Name,
		Nonce:         tok.Nonce,
	}, nil
}

// isTrue reports whether a JSON claim is true or "true": Apple sends
// email_verified as a string, Google as a boolean.
func isTrue(raw json.RawMessage) bool {
	s := string(raw)
	return s == "true" || s == `"true"`
}
