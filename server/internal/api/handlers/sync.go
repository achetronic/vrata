package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/achetronic/rutoso/internal/model"
)

// SyncStream streams configuration snapshots to proxy-mode instances via
// Server-Sent Events (SSE). The stream serves the active versioned snapshot.
// If no snapshot is active, it returns 503 until one is activated.
// On connect the client receives the active snapshot immediately. After that,
// every snapshot change (activate, delete) triggers a new push.
//
// @Summary     SSE sync stream
// @Description Streams the active versioned snapshot in real time. Designed for proxy-mode instances.
// @Tags        sync
// @Produce     text/event-stream
// @Success     200 {object} model.Snapshot
// @Failure     503 {object} respond.ErrorBody
// @Router      /sync/stream [get]
func (d *Dependencies) SyncStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()

	if err := sendActiveSnapshot(ctx, w, flusher, d); err != nil {
		if !errors.Is(err, model.ErrNoActiveSnapshot) {
			d.Logger.Error("sync: initial snapshot failed",
				slog.String("error", err.Error()),
			)
		}
	}

	events, err := d.Store.Subscribe(ctx)
	if err != nil {
		d.Logger.Error("sync: subscribing to store",
			slog.String("error", err.Error()),
		)
		return
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-events:
			if !ok {
				return
			}
			if ev.Resource != "snapshot" {
				continue
			}
			if err := sendActiveSnapshot(ctx, w, flusher, d); err != nil {
				if errors.Is(err, model.ErrNoActiveSnapshot) {
					continue
				}
				d.Logger.Error("sync: sending snapshot",
					slog.String("error", err.Error()),
				)
				return
			}
		}
	}
}

// sendActiveSnapshot reads the active versioned snapshot and writes it as an SSE event.
func sendActiveSnapshot(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, d *Dependencies) error {
	vs, err := d.Store.GetActiveSnapshot(ctx)
	if err != nil {
		return err
	}

	data, err := json.Marshal(vs.Snapshot)
	if err != nil {
		return err
	}

	if _, err := w.Write([]byte("event: snapshot\ndata: ")); err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if _, err := w.Write([]byte("\n\n")); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// buildSnapshot reads all entities from the store into a Snapshot.
// Used by CreateSnapshot to capture the current live state.
func buildSnapshot(ctx context.Context, d *Dependencies) (*model.Snapshot, error) {
	listeners, err := d.Store.ListListeners(ctx)
	if err != nil {
		return nil, err
	}
	routes, err := d.Store.ListRoutes(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := d.Store.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	destinations, err := d.Store.ListDestinations(ctx)
	if err != nil {
		return nil, err
	}
	middlewares, err := d.Store.ListMiddlewares(ctx)
	if err != nil {
		return nil, err
	}

	return &model.Snapshot{
		Listeners:    listeners,
		Routes:       routes,
		Groups:       groups,
		Destinations: destinations,
		Middlewares:  middlewares,
	}, nil
}
