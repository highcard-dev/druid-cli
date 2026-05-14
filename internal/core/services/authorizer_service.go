package services

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/MicahParks/keyfunc"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v4"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

const queryTokenTTL = 5 * time.Minute

type AuthorizerService struct {
	jwksUrls   []string
	jwks       []*keyfunc.JWKS
	userId     string
	runtimeKey *rsa.PrivateKey
	keyID      string
	mu         sync.Mutex
}

func NewAuthorizer(jwksURLs []string, userId string) (ports.AuthorizerServiceInterface, error) {
	auth, err := newAuthorizer(jwksURLs, userId, true)
	if err != nil {
		return nil, err
	}
	return auth, nil
}

func NewRuntimeTokenVerifier(jwksURL string) (ports.AuthorizerServiceInterface, error) {
	if strings.TrimSpace(jwksURL) == "" {
		return NewAuthorizer(nil, "")
	}
	auth, err := newAuthorizer([]string{jwksURL}, "", false)
	if err != nil {
		return nil, err
	}
	return auth, nil
}

func newAuthorizer(jwksURLs []string, userId string, generateRuntimeKey bool) (*AuthorizerService, error) {
	jwksURLs = normalizeJWKSURLs(jwksURLs)
	auth := &AuthorizerService{
		jwksUrls: jwksURLs,
		userId:   userId,
	}

	if len(jwksURLs) > 0 {
		options := keyfunc.Options{
			RefreshInterval: time.Hour,
			RefreshErrorHandler: func(err error) {
				logger.Log().Error("There was an error with the jwt.KeyFunc", zap.Error(err))
			},
		}
		auth.jwks = make([]*keyfunc.JWKS, 0, len(jwksURLs))
		for _, jwksURL := range jwksURLs {
			jwks, err := keyfunc.Get(jwksURL, options)
			if err != nil {
				return nil, fmt.Errorf("failed to get JWKS from %q: %w", jwksURL, err)
			}
			auth.jwks = append(auth.jwks, jwks)
		}
	}

	if generateRuntimeKey {
		auth.ensureRuntimeKey()
	}
	return auth, nil
}

func normalizeJWKSURLs(jwksURLs []string) []string {
	normalized := make([]string, 0, len(jwksURLs))
	seen := make(map[string]struct{}, len(jwksURLs))
	for _, jwksURL := range jwksURLs {
		jwksURL = strings.TrimSpace(jwksURL)
		if jwksURL == "" {
			continue
		}
		if _, ok := seen[jwksURL]; ok {
			continue
		}
		seen[jwksURL] = struct{}{}
		normalized = append(normalized, jwksURL)
	}
	return normalized
}

func (auth *AuthorizerService) CheckHeader(c *fiber.Ctx) (*ports.AuthContext, error) {
	if len(auth.jwksUrls) == 0 {
		return nil, nil
	}

	reqToken := c.Request().Header.Peek("Authorization")
	splitToken := strings.Split(string(reqToken), "Bearer ")
	if len(splitToken) != 2 {
		return nil, errors.New("malformed or missing token")
	}

	token, err := auth.parseJWT(splitToken[1])
	if err != nil {
		return nil, errors.New("Failed to parse the JWT.\nError: " + err.Error())
	}
	if !token.Valid {
		return nil, errors.New("the token is not valid")
	}

	claims, _ := token.Claims.(jwt.MapClaims)
	if claims == nil {
		return nil, errors.New("couldn't parse claims")
	}
	if auth.userId != "" && claims["sub"] != auth.userId {
		return nil, errors.New("error checking user id")
	}

	var expiresAt *time.Time
	if expires, ok := claimTime(claims["exp"]); ok {
		expiresAt = &expires
	}
	subject, _ := claims["sub"].(string)
	return &ports.AuthContext{Subject: subject, ExpiresAt: expiresAt}, nil
}

func (auth *AuthorizerService) parseJWT(jwtToken string) (*jwt.Token, error) {
	var parseErrors []string
	for index, jwks := range auth.jwks {
		token, err := jwt.Parse(jwtToken, jwks.Keyfunc)
		if err == nil && token.Valid {
			return token, nil
		}
		if err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("JWKS %d: %s", index+1, err.Error()))
			continue
		}
		parseErrors = append(parseErrors, fmt.Sprintf("JWKS %d: token is not valid", index+1))
	}
	if len(parseErrors) == 0 {
		return nil, errors.New("jwt verifier is not configured")
	}
	return nil, errors.New(strings.Join(parseErrors, "; "))
}

func (auth *AuthorizerService) CheckQuery(runtimeID string, tokenString string) (*ports.AuthContext, error) {
	if tokenString == "" {
		return nil, errors.New("missing token")
	}

	var token *jwt.Token
	var err error
	if auth.runtimeKey != nil {
		token, err = jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
			return &auth.runtimeKey.PublicKey, nil
		})
	} else {
		token, err = auth.parseJWT(tokenString)
	}
	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}

	claims, _ := token.Claims.(jwt.MapClaims)
	if claims == nil {
		return nil, errors.New("couldn't parse claims")
	}
	if expected, _ := claims["runtime_id"].(string); runtimeID != "" && expected != runtimeID {
		return nil, errors.New("runtime token does not match runtime")
	}
	expires, ok := claimTime(claims["exp"])
	if !ok || time.Now().After(expires) {
		return nil, errors.New("runtime token expired")
	}
	subject, _ := claims["sub"].(string)
	claimRuntimeID, _ := claims["runtime_id"].(string)
	return &ports.AuthContext{Subject: subject, RuntimeID: claimRuntimeID, ExpiresAt: &expires}, nil
}

func (auth *AuthorizerService) GenerateQueryToken(runtimeID string, ownerID string) string {
	auth.ensureRuntimeKey()
	expires := time.Now().Add(queryTokenTTL)
	claims := jwt.MapClaims{
		"sub":        ownerID,
		"runtime_id": runtimeID,
		"scope":      "runtime",
		"exp":        expires.Unix(),
		"iat":        time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = auth.keyID
	signed, err := token.SignedString(auth.runtimeKey)
	if err != nil {
		logger.Log().Error("failed to sign runtime query token", zap.Error(err))
		return ""
	}
	return signed
}

func (auth *AuthorizerService) JWKS() map[string]any {
	auth.ensureRuntimeKey()
	pub := auth.runtimeKey.PublicKey
	return map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA",
			"use": "sig",
			"kid": auth.keyID,
			"alg": "RS256",
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}},
	}
}

func (auth *AuthorizerService) ensureRuntimeKey() {
	auth.mu.Lock()
	defer auth.mu.Unlock()
	if auth.runtimeKey != nil {
		return
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		logger.Log().Error("failed to generate runtime token key", zap.Error(err))
		return
	}
	keyID, _ := utils.GenerateRandomStringURLSafe(12)
	auth.runtimeKey = key
	auth.keyID = keyID
}

func claimTime(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case float64:
		return time.Unix(int64(typed), 0), true
	case json.Number:
		v, _ := typed.Int64()
		return time.Unix(v, 0), true
	default:
		return time.Time{}, false
	}
}
