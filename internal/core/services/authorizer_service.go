package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v4"
	"github.com/highcard-dev/daemon/internal/core/ports"
	"github.com/highcard-dev/daemon/internal/utils"
	"github.com/highcard-dev/daemon/internal/utils/logger"
	"go.uber.org/zap"
)

type AuthorizerService struct {
	jwksUrls []string
	jwks     []*keyfunc.JWKS
	userId   string
	tokens   map[string]time.Time
}

func NewAuthorizer(jwksURLs []string, userId string) (ports.AuthorizerServiceInterface, error) {
	jwksURLs = normalizeJWKSURLs(jwksURLs)

	// Create the keyfunc options. Refresh the JWKS every hour and log errors.
	var refreshInterval = time.Hour
	var options = keyfunc.Options{
		RefreshInterval: refreshInterval,
		RefreshErrorHandler: func(err error) {
			logger.Log().Error("There was an error with the jwt.KeyFunc", zap.Error(err))
		},
	}

	if len(jwksURLs) > 0 {
		jwksList := make([]*keyfunc.JWKS, 0, len(jwksURLs))
		for _, jwksURL := range jwksURLs {
			// Create the JWKS from the resource at the given URL.
			jwks, err := keyfunc.Get(jwksURL, options)
			if err != nil {
				return nil, fmt.Errorf("failed to get JWKS from %q: %w", jwksURL, err)
			}
			jwksList = append(jwksList, jwks)
		}

		return &AuthorizerService{
			jwks:     jwksList,
			jwksUrls: jwksURLs,
			userId:   userId,
			tokens:   make(map[string]time.Time),
		}, nil

	} else {
		return &AuthorizerService{
			tokens: make(map[string]time.Time),
		}, nil
	}
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

func (auth *AuthorizerService) CheckHeader(c *fiber.Ctx) (*time.Time, error) {

	if len(auth.jwksUrls) == 0 {
		return nil, nil
	}

	// Get a JWT to parse.
	reqToken := c.Request().Header.Peek("Authorization")
	splitToken := strings.Split(string(reqToken), "Bearer ")

	if len(splitToken) != 2 {
		return nil, errors.New("malformed or missing token")
	}

	jwtToken := splitToken[1]

	// Parse the JWT.
	token, err := auth.parseJWT(jwtToken)
	if err != nil {
		return nil, errors.New("Failed to parse the JWT.\nError: " + err.Error())
	}

	// Check if the token is valid.
	if !token.Valid {
		return nil, errors.New("the token is not valid")
	}

	claims, _ := token.Claims.(jwt.MapClaims)
	if claims == nil {
		return nil, errors.New("couldn't parse claims")
	}

	if auth.userId != "" {
		if claims["sub"] != auth.userId {
			return nil, errors.New("error checking user id")
		}
	}

	var tm time.Time
	switch iat := claims["exp"].(type) {
	case float64:
		tm = time.Unix(int64(iat), 0)
	case json.Number:
		v, _ := iat.Int64()
		tm = time.Unix(v, 0)
	}

	return &tm, nil
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
	return nil, errors.New(strings.Join(parseErrors, "; "))
}

func (auth *AuthorizerService) CheckQuery(token string) (*time.Time, error) {
	if validUntil, ok := auth.tokens[token]; ok {
		defer delete(auth.tokens, token)
		if validUntil.After(time.Now()) {
			return &validUntil, nil
		}
	}
	return nil, errors.New("no valid token found")

}

func (auth *AuthorizerService) GenerateQueryToken() string {

	token, _ := utils.GenerateRandomStringURLSafe(16)

	//TODO: it is not required to save the expire date in the map bcs of the cleanup below
	auth.tokens[token] = time.Now().Add(time.Minute * 5) // TODO: configuration

	t := time.NewTimer(time.Minute * 5)
	go func() {
		<-t.C
		delete(auth.tokens, token)
	}()

	return token
}
