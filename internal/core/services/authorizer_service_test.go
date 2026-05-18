package services

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v4"
)

func TestAuthorizerService_CheckHeaderAcceptsMultipleJWKS(t *testing.T) {
	userID := "user-123"
	kid := "migration-key"
	oldKey := mustGenerateRSAKey(t)
	newKey := mustGenerateRSAKey(t)

	oldJWKS := newJWKSServer(t, kid, &oldKey.PublicKey)
	newJWKS := newJWKSServer(t, kid, &newKey.PublicKey)

	authorizer, err := NewAuthorizer([]string{oldJWKS.URL, newJWKS.URL}, userID)
	if err != nil {
		t.Fatalf("NewAuthorizer returned error: %v", err)
	}

	token := mustSignJWT(t, kid, userID, newKey)

	if err := checkAuthorizationHeader(authorizer, token); err != nil {
		t.Fatalf("expected token signed by second JWKS to validate: %v", err)
	}
}

func TestAuthorizerService_CheckHeaderRejectsUnknownJWKS(t *testing.T) {
	userID := "user-123"
	kid := "migration-key"
	oldKey := mustGenerateRSAKey(t)
	newKey := mustGenerateRSAKey(t)
	otherKey := mustGenerateRSAKey(t)

	oldJWKS := newJWKSServer(t, kid, &oldKey.PublicKey)
	newJWKS := newJWKSServer(t, kid, &newKey.PublicKey)

	authorizer, err := NewAuthorizer([]string{oldJWKS.URL, newJWKS.URL}, userID)
	if err != nil {
		t.Fatalf("NewAuthorizer returned error: %v", err)
	}

	token := mustSignJWT(t, kid, userID, otherKey)

	if err := checkAuthorizationHeader(authorizer, token); err == nil {
		t.Fatal("expected token signed by an unknown JWKS to be rejected")
	}
}

func TestAuthorizerService_CheckHeaderRejectsWrongSub(t *testing.T) {
	kid := "migration-key"
	key := mustGenerateRSAKey(t)
	jwks := newJWKSServer(t, kid, &key.PublicKey)

	authorizer, err := NewAuthorizer([]string{jwks.URL}, "expected-user")
	if err != nil {
		t.Fatalf("NewAuthorizer returned error: %v", err)
	}

	token := mustSignJWT(t, kid, "other-user", key)

	if err := checkAuthorizationHeader(authorizer, token); err == nil {
		t.Fatal("expected token with wrong sub claim to be rejected")
	}
}

func checkAuthorizationHeader(authorizer interface {
	CheckHeader(*fiber.Ctx) (*time.Time, error)
}, token string) error {
	app := fiber.New()
	var authErr error
	app.Get("/", func(c *fiber.Ctx) error {
		_, authErr = authorizer.CheckHeader(c)
		if authErr != nil {
			return authErr
		}
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return authErr
}

func newJWKSServer(t *testing.T, kid string, publicKey *rsa.PublicKey) *httptest.Server {
	t.Helper()

	jwks := map[string]any{
		"keys": []map[string]string{
			{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": kid,
				"n":   base64.RawURLEncoding.EncodeToString(publicKey.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(publicKey.E)).Bytes()),
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(jwks); err != nil {
			t.Errorf("failed to encode JWKS: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func mustGenerateRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	return key
}

func mustSignJWT(t *testing.T, kid string, userID string, key *rsa.PrivateKey) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": userID,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	token.Header["kid"] = kid

	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("failed to sign JWT: %v", err)
	}
	return signed
}
