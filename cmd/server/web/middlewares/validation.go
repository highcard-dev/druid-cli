package middlewares

import (
	"bytes"
	"context"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/gofiber/fiber/v2"
	"github.com/highcard-dev/daemon/internal/api"
)

// OpenAPIValidator middleware validates incoming requests against the OpenAPI specification
type OpenAPIValidator struct {
	router routers.Router
	spec   *openapi3.T
}

// NewOpenAPIValidator creates a new OpenAPI validation middleware
func NewOpenAPIValidator() (*OpenAPIValidator, error) {
	swagger, err := api.GetSwagger()
	if err != nil {
		return nil, err
	}

	// Create router for finding routes
	router, err := gorillamux.NewRouter(swagger)
	if err != nil {
		return nil, err
	}

	return &OpenAPIValidator{
		router: router,
		spec:   swagger,
	}, nil
}

// Middleware returns a Fiber middleware handler that validates requests
func (v *OpenAPIValidator) Middleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Convert Fiber context to http.Request
		req, err := fiberToHTTPRequest(c)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"status": "error",
				"error":  "Failed to process request",
			})
		}

		// Find the route in OpenAPI spec
		route, pathParams, err := v.router.FindRoute(req)
		if err != nil {
			// Route not found in OpenAPI spec, skip validation
			return c.Next()
		}

		// Validate request
		requestValidationInput := &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: pathParams,
			Route:      route,
			Options: &openapi3filter.Options{
				AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
			},
		}

		ctx := context.Background()
		if err := openapi3filter.ValidateRequest(ctx, requestValidationInput); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"status": "error",
				"error":  err.Error(),
			})
		}

		return c.Next()
	}
}

// fiberToHTTPRequest converts a Fiber context to a standard http.Request
func fiberToHTTPRequest(c *fiber.Ctx) (*http.Request, error) {
	// Get the request body
	body := c.Body()
	bodyReader := bytes.NewReader(body)

	// Create the HTTP request
	method := c.Method()
	url := c.OriginalURL()

	// Build full URL with scheme and host
	scheme := "http"
	if c.Protocol() == "https" {
		scheme = "https"
	}
	fullURL := scheme + "://" + c.Hostname() + url

	req, err := http.NewRequest(method, fullURL, bodyReader)
	if err != nil {
		return nil, err
	}

	// Copy headers
	c.Request().Header.VisitAll(func(key, value []byte) {
		req.Header.Add(string(key), string(value))
	})

	// Set Content-Type if present
	contentType := c.Get("Content-Type")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	return req, nil
}

// MustNewOpenAPIValidator creates a new validator or panics on error
func MustNewOpenAPIValidator() *OpenAPIValidator {
	validator, err := NewOpenAPIValidator()
	if err != nil {
		panic(err)
	}
	return validator
}
