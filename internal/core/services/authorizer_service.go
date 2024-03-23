package services

import (
	"encoding/json"
	"errors"
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
	jwksUrl string
	jwks    *keyfunc.JWKS
	userId  string
	tokens  map[string]time.Time
}

func NewAuthorizer(jwksURL string, userId string) (ports.AuthorizerServiceInterface, error) {

	// Create the keyfunc options. Refresh the JWKS every hour and log errors.
	var refreshInterval = time.Hour
	var options = keyfunc.Options{
		RefreshInterval: refreshInterval,
		RefreshErrorHandler: func(err error) {
			logger.Log().Error("There was an error with the jwt.KeyFunc", zap.Error(err))
		},
	}

	if jwksURL != "" {
		// Create the JWKS from the resource at the given URL.
		var jwks, err = keyfunc.Get(jwksURL, options)
		if err != nil {
			return nil, err
		}

		return &AuthorizerService{
			jwks:    jwks,
			jwksUrl: jwksURL,
			userId:  userId,
			tokens:  make(map[string]time.Time),
		}, nil

	} else {
		return &AuthorizerService{
			tokens: make(map[string]time.Time),
		}, nil
	}
}

func (auth *AuthorizerService) CheckHeader(c *fiber.Ctx) (*time.Time, error) {

	if auth.jwksUrl == "" {
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
	token, err := jwt.Parse(jwtToken, auth.jwks.Keyfunc)

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
