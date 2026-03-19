// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

// Package sync implements the SSE client that proxy-mode instances use to
// receive configuration snapshots from a remote control plane. Snapshots are
// applied atomically — in-flight requests on the old config complete
// undisturbed while new requests use the new config.
package sync

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/achetronic/vrata/internal/model"
	"github.com/achetronic/vrata/internal/proxy"
)

// Dependencies holds the external collaborators required by the sync client.
type Dependencies struct {
	ControlPlaneAddr  string
	ReconnectInterval time.Duration
	Router            *proxy.Router
	ListenerManager   *proxy.ListenerManager
	HealthChecker     *proxy.HealthChecker
	OutlierDetector   *proxy.OutlierDetector
	SessionStore      proxy.SessionStore
	Logger            *slog.Logger
}

// Client connects to the control plane SSE stream and applies configuration
// snapshots to the local proxy.
type Client struct {
	deps Dependencies
}

// New creates a new sync Client.
func New(deps Dependencies) *Client {
	return &Client{deps: deps}
}

// Run connects to the control plane and processes snapshots until ctx is
// cancelled. On disconnection it reconnects automatically after the
// configured interval.
func (c *Client) Run(ctx context.Context) error {
	for {
		err := c.stream(ctx)
		if ctx.Err() != nil {
			return nil
		}
		c.deps.Logger.Warn("sync: stream disconnected, reconnecting",
			slog.String("error", err.Error()),
			slog.String("interval", c.deps.ReconnectInterval.String()),
		)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(c.deps.ReconnectInterval):
		}
	}
}

// stream opens one SSE connection and processes events until error or ctx cancel.
func (c *Client) stream(ctx context.Context) error {
	url := strings.TrimRight(c.deps.ControlPlaneAddr, "/") + "/api/v1/sync/snapshot"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{
		Timeout: 0,
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connecting to control plane: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("control plane returned %d", resp.StatusCode)
	}

	c.deps.Logger.Info("sync: connected to control plane",
		slog.String("url", url),
	)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 4*1024*1024), 4*1024*1024)

	var eventType string

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			if eventType == "snapshot" {
				continue
			}
			eventType = ""
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") && eventType == "snapshot" {
			data := strings.TrimPrefix(line, "data: ")
			if err := c.applySnapshot([]byte(data)); err != nil {
				c.deps.Logger.Error("sync: applying snapshot",
					slog.String("error", err.Error()),
				)
			}
			eventType = ""
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading stream: %w", err)
	}
	return fmt.Errorf("stream closed by server")
}

// applySnapshot deserialises a snapshot and swaps the proxy config atomically.
func (c *Client) applySnapshot(data []byte) error {
	var vs model.VersionedSnapshot
	if err := json.Unmarshal(data, &vs); err != nil {
		return fmt.Errorf("decoding snapshot: %w", err)
	}

	snap := vs.Snapshot

	table, err := proxy.BuildTable(snap.Routes, snap.Groups, snap.Destinations, snap.Middlewares, c.deps.SessionStore)
	if err != nil {
		return fmt.Errorf("building routing table: %w", err)
	}

	c.deps.Router.SwapTable(table)

	if c.deps.HealthChecker != nil {
		c.deps.HealthChecker.Update(table.Pools())
	}
	if c.deps.OutlierDetector != nil {
		c.deps.OutlierDetector.Update(table.Pools())
		od := c.deps.OutlierDetector
		for _, pool := range table.Pools() {
			for _, ep := range pool.Endpoints {
				ep.OnResponse = od.RecordResponse
			}
			pool.OnResponse = od.RecordResponse
		}
	}

	c.deps.ListenerManager.Reconcile(snap.Listeners)

	c.deps.Logger.Info("sync: snapshot applied",
		slog.String("id", vs.ID),
		slog.String("name", vs.Name),
		slog.Int("listeners", len(snap.Listeners)),
		slog.Int("routes", len(snap.Routes)),
		slog.Int("groups", len(snap.Groups)),
		slog.Int("destinations", len(snap.Destinations)),
		slog.Int("middlewares", len(snap.Middlewares)),
	)

	return nil
}
