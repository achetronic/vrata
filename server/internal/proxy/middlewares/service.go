package middlewares

import "net/http"

// Service represents an external service that a middleware can connect to.
// This decouples middlewares from the proxy.Endpoint type.
type Service struct {
	// BaseURL is the full scheme://host:port of the service.
	BaseURL   string
	// Transport is the configured HTTP transport (with TLS, timeouts, etc.).
	Transport http.RoundTripper
}
