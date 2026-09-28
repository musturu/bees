package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
)

const testIssuer = "https://issuer.example.test"

func newTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func signClaims(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	jws, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jws.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func validClaims() map[string]any {
	return map[string]any{
		"iss":            testIssuer,
		"sub":            "user-123",
		"aud":            "android-client",
		"exp":            time.Now().Add(time.Hour).Unix(),
		"iat":            time.Now().Unix(),
		"email":          "vol@example.test",
		"email_verified": true,
		"name":           "Mario Rossi",
		"nonce":          "n-1",
	}
}

func TestIDTokenVerifier(t *testing.T) {
	key := newTestKey(t)
	v := newIDTokenVerifier(testIssuer, &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}},
		"ios-client", "android-client")

	t.Run("valid token", func(t *testing.T) {
		claims, err := v.Verify(t.Context(), signClaims(t, key, validClaims()))
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		want := IDClaims{Subject: "user-123", Email: "vol@example.test", EmailVerified: true, Name: "Mario Rossi", Nonce: "n-1"}
		if claims != want {
			t.Fatalf("claims = %+v, want %+v", claims, want)
		}
	})

	t.Run("audience list containing a client id", func(t *testing.T) {
		c := validClaims()
		c["aud"] = []string{"someone-else", "ios-client"}
		if _, err := v.Verify(t.Context(), signClaims(t, key, c)); err != nil {
			t.Fatalf("Verify: %v", err)
		}
	})

	emailVerified := []struct {
		value any
		want  bool
	}{{true, true}, {"true", true}, {false, false}, {"false", false}, {nil, false}}
	for _, tt := range emailVerified {
		t.Run("email_verified", func(t *testing.T) {
			c := validClaims()
			if tt.value == nil {
				delete(c, "email_verified")
			} else {
				c["email_verified"] = tt.value
			}
			claims, err := v.Verify(t.Context(), signClaims(t, key, c))
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if claims.EmailVerified != tt.want {
				t.Fatalf("email_verified %#v: got %v, want %v", tt.value, claims.EmailVerified, tt.want)
			}
		})
	}

	otherKey := newTestKey(t)
	rejects := []struct {
		name  string
		key   *rsa.PrivateKey
		patch func(map[string]any)
	}{
		{"wrong issuer", key, func(c map[string]any) { c["iss"] = "https://evil.example.test" }},
		{"wrong audience", key, func(c map[string]any) { c["aud"] = "someone-else" }},
		{"expired", key, func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() }},
		{"bad signature", otherKey, func(map[string]any) {}},
		{"no subject", key, func(c map[string]any) { delete(c, "sub") }},
	}
	for _, tt := range rejects {
		t.Run(tt.name, func(t *testing.T) {
			c := validClaims()
			tt.patch(c)
			if _, err := v.Verify(t.Context(), signClaims(t, tt.key, c)); err == nil {
				t.Fatal("Verify succeeded, want an error")
			}
		})
	}

	t.Run("garbage", func(t *testing.T) {
		if _, err := v.Verify(t.Context(), "not-a-jwt"); err == nil {
			t.Fatal("Verify succeeded, want an error")
		}
	})
}
