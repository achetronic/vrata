// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package vrata provides a typed HTTP client for the Vrata REST API.
// The client is the only interface between the controller and Vrata —
// all synchronisation goes through these methods.
package vrata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Entity represents any Vrata resource with an ID and a Name.
type Entity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Route is a minimal representation of a Vrata Route for sync purposes.
type Route struct {
	ID                  string         `json:"id,omitempty"`
	Name                string         `json:"name"`
	Match               map[string]any `json:"match"`
	Forward             map[string]any `json:"forward,omitempty"`
	Redirect            map[string]any `json:"redirect,omitempty"`
	DirectResponse      map[string]any `json:"directResponse,omitempty"`
	MiddlewareIDs       []string       `json:"middlewareIds,omitempty"`
	MiddlewareOverrides map[string]any `json:"middlewareOverrides,omitempty"`
}

// RouteGroup is a minimal representation of a Vrata RouteGroup.
type RouteGroup struct {
	ID        string   `json:"id,omitempty"`
	Name      string   `json:"name"`
	RouteIDs  []string `json:"routeIds"`
	Hostnames []string `json:"hostnames,omitempty"`
}

// Destination is a minimal representation of a Vrata Destination.
type Destination struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
	Host string `json:"host"`
	Port uint32 `json:"port"`
}

// Listener is a minimal representation of a Vrata Listener.
type Listener struct {
	ID      string         `json:"id,omitempty"`
	Name    string         `json:"name"`
	Address string         `json:"address,omitempty"`
	Port    uint32         `json:"port"`
	TLS     map[string]any `json:"tls,omitempty"`
}

// Middleware is a minimal representation of a Vrata Middleware.
type Middleware struct {
	ID      string         `json:"id,omitempty"`
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Headers map[string]any `json:"headers,omitempty"`
}

// Snapshot is a minimal representation of a Vrata VersionedSnapshot.
type Snapshot struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Client is a typed HTTP client for the Vrata REST API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
}

// Option configures the Client.
type Option func(*Client)

// WithHTTPClient sets the underlying HTTP client (e.g. for TLS).
func WithHTTPClient(c *http.Client) Option {
	return func(cl *Client) { cl.httpClient = c }
}

// WithAPIKey sets the bearer token sent in the Authorization header.
func WithAPIKey(key string) Option {
	return func(cl *Client) { cl.apiKey = key }
}

// NewClient creates a Vrata API client pointing at the given base URL.
func NewClient(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Ping checks whether the Vrata API is reachable by hitting a lightweight
// endpoint. Returns nil on success, an error if the API is unreachable.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/groups", nil)
	if err != nil {
		return fmt.Errorf("building ping request: %w", err)
	}
	c.setAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("ping: server returned %d", resp.StatusCode)
	}
	return nil
}

// ─── Routes ─────────────────────────────────────────────────────────────────

// ListRoutes returns all routes from Vrata.
func (c *Client) ListRoutes(ctx context.Context) ([]Route, error) {
	var routes []Route
	if err := c.get(ctx, "/api/v1/routes", &routes); err != nil {
		return nil, fmt.Errorf("listing routes: %w", err)
	}
	return routes, nil
}

// CreateRoute creates a route and returns the created entity with its ID.
func (c *Client) CreateRoute(ctx context.Context, r Route) (*Route, error) {
	var created Route
	if err := c.post(ctx, "/api/v1/routes", r, &created); err != nil {
		return nil, fmt.Errorf("creating route %q: %w", r.Name, err)
	}
	return &created, nil
}

// UpdateRoute replaces a route by ID.
func (c *Client) UpdateRoute(ctx context.Context, id string, r Route) error {
	if err := c.put(ctx, "/api/v1/routes/"+id, r); err != nil {
		return fmt.Errorf("updating route %q: %w", id, err)
	}
	return nil
}

// DeleteRoute removes a route by ID.
func (c *Client) DeleteRoute(ctx context.Context, id string) error {
	if err := c.del(ctx, "/api/v1/routes/"+id); err != nil {
		return fmt.Errorf("deleting route %q: %w", id, err)
	}
	return nil
}

// ─── RouteGroups ────────────────────────────────────────────────────────────

// ListGroups returns all route groups from Vrata.
func (c *Client) ListGroups(ctx context.Context) ([]RouteGroup, error) {
	var groups []RouteGroup
	if err := c.get(ctx, "/api/v1/groups", &groups); err != nil {
		return nil, fmt.Errorf("listing groups: %w", err)
	}
	return groups, nil
}

// CreateGroup creates a route group and returns the created entity.
func (c *Client) CreateGroup(ctx context.Context, g RouteGroup) (*RouteGroup, error) {
	var created RouteGroup
	if err := c.post(ctx, "/api/v1/groups", g, &created); err != nil {
		return nil, fmt.Errorf("creating group %q: %w", g.Name, err)
	}
	return &created, nil
}

// UpdateGroup replaces a route group by ID.
func (c *Client) UpdateGroup(ctx context.Context, id string, g RouteGroup) error {
	if err := c.put(ctx, "/api/v1/groups/"+id, g); err != nil {
		return fmt.Errorf("updating group %q: %w", id, err)
	}
	return nil
}

// DeleteGroup removes a route group by ID.
func (c *Client) DeleteGroup(ctx context.Context, id string) error {
	if err := c.del(ctx, "/api/v1/groups/"+id); err != nil {
		return fmt.Errorf("deleting group %q: %w", id, err)
	}
	return nil
}

// ─── Destinations ───────────────────────────────────────────────────────────

// ListDestinations returns all destinations from Vrata.
func (c *Client) ListDestinations(ctx context.Context) ([]Destination, error) {
	var dests []Destination
	if err := c.get(ctx, "/api/v1/destinations", &dests); err != nil {
		return nil, fmt.Errorf("listing destinations: %w", err)
	}
	return dests, nil
}

// CreateDestination creates a destination and returns the created entity.
func (c *Client) CreateDestination(ctx context.Context, d Destination) (*Destination, error) {
	var created Destination
	if err := c.post(ctx, "/api/v1/destinations", d, &created); err != nil {
		return nil, fmt.Errorf("creating destination %q: %w", d.Name, err)
	}
	return &created, nil
}

// UpdateDestination replaces a destination by ID.
func (c *Client) UpdateDestination(ctx context.Context, id string, d Destination) error {
	if err := c.put(ctx, "/api/v1/destinations/"+id, d); err != nil {
		return fmt.Errorf("updating destination %q: %w", id, err)
	}
	return nil
}

// DeleteDestination removes a destination by ID.
func (c *Client) DeleteDestination(ctx context.Context, id string) error {
	if err := c.del(ctx, "/api/v1/destinations/"+id); err != nil {
		return fmt.Errorf("deleting destination %q: %w", id, err)
	}
	return nil
}

// ─── Listeners ──────────────────────────────────────────────────────────────

// ListListeners returns all listeners from Vrata.
func (c *Client) ListListeners(ctx context.Context) ([]Listener, error) {
	var listeners []Listener
	if err := c.get(ctx, "/api/v1/listeners", &listeners); err != nil {
		return nil, fmt.Errorf("listing listeners: %w", err)
	}
	return listeners, nil
}

// CreateListener creates a listener and returns the created entity.
func (c *Client) CreateListener(ctx context.Context, l Listener) (*Listener, error) {
	var created Listener
	if err := c.post(ctx, "/api/v1/listeners", l, &created); err != nil {
		return nil, fmt.Errorf("creating listener %q: %w", l.Name, err)
	}
	return &created, nil
}

// UpdateListener replaces a listener by ID.
func (c *Client) UpdateListener(ctx context.Context, id string, l Listener) error {
	if err := c.put(ctx, "/api/v1/listeners/"+id, l); err != nil {
		return fmt.Errorf("updating listener %q: %w", id, err)
	}
	return nil
}

// DeleteListener removes a listener by ID.
func (c *Client) DeleteListener(ctx context.Context, id string) error {
	if err := c.del(ctx, "/api/v1/listeners/"+id); err != nil {
		return fmt.Errorf("deleting listener %q: %w", id, err)
	}
	return nil
}

// ─── Middlewares ─────────────────────────────────────────────────────────────

// ListMiddlewares returns all middlewares from Vrata.
func (c *Client) ListMiddlewares(ctx context.Context) ([]Middleware, error) {
	var mws []Middleware
	if err := c.get(ctx, "/api/v1/middlewares", &mws); err != nil {
		return nil, fmt.Errorf("listing middlewares: %w", err)
	}
	return mws, nil
}

// CreateMiddleware creates a middleware and returns the created entity.
func (c *Client) CreateMiddleware(ctx context.Context, m Middleware) (*Middleware, error) {
	var created Middleware
	if err := c.post(ctx, "/api/v1/middlewares", m, &created); err != nil {
		return nil, fmt.Errorf("creating middleware %q: %w", m.Name, err)
	}
	return &created, nil
}

// DeleteMiddleware removes a middleware by ID.
func (c *Client) DeleteMiddleware(ctx context.Context, id string) error {
	if err := c.del(ctx, "/api/v1/middlewares/"+id); err != nil {
		return fmt.Errorf("deleting middleware %q: %w", id, err)
	}
	return nil
}

// UpdateMiddleware replaces a middleware by ID.
func (c *Client) UpdateMiddleware(ctx context.Context, id string, m Middleware) error {
	if err := c.put(ctx, "/api/v1/middlewares/"+id, m); err != nil {
		return fmt.Errorf("updating middleware %q: %w", id, err)
	}
	return nil
}

// ─── Snapshots ──────────────────────────────────────────────────────────────

// CreateSnapshot creates a versioned snapshot and returns it.
func (c *Client) CreateSnapshot(ctx context.Context, name string) (*Snapshot, error) {
	var snap Snapshot
	if err := c.post(ctx, "/api/v1/snapshots", map[string]string{"name": name}, &snap); err != nil {
		return nil, fmt.Errorf("creating snapshot %q: %w", name, err)
	}
	return &snap, nil
}

// ActivateSnapshot activates a snapshot by ID.
func (c *Client) ActivateSnapshot(ctx context.Context, id string) error {
	if err := c.post(ctx, "/api/v1/snapshots/"+id+"/activate", nil, nil); err != nil {
		return fmt.Errorf("activating snapshot %q: %w", id, err)
	}
	return nil
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// APIError is returned when Vrata responds with a non-2xx status.
type APIError struct {
	StatusCode int
	Body       string
}

// Error returns the formatted API error string.
func (e *APIError) Error() string {
	return fmt.Sprintf("vrata API error %d: %s", e.StatusCode, e.Body)
}

// setAuth adds the Authorization header if an API key is configured.
func (c *Client) setAuth(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

// get performs a GET request and decodes the response.
func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	c.setAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// post performs a POST request with a JSON body and decodes the response.
func (c *Client) post(ctx context.Context, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding body: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return &APIError{StatusCode: resp.StatusCode, Body: string(b)}
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// put performs a PUT request with a JSON body.
func (c *Client) put(ctx context.Context, path string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encoding body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// del performs a DELETE request.
func (c *Client) del(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	c.setAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return &APIError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
