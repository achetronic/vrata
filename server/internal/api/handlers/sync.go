package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/achetronic/rutoso/internal/model"
)

// SyncStream streams configuration snapshots to proxy-mode instances via
// Server-Sent Events (SSE). On connect the client receives an immediate
// full snapshot. After that, every store change triggers a new snapshot.
//
// @Summary     SSE sync stream
// @Description Streams full configuration snapshots in real time. Designed for proxy-mode instances.
// @Tags        sync
// @Produce     text/event-stream
// @Success     200 {object} model.Snapshot
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

	if err := sendSnapshot(ctx, w, flusher, d); err != nil {
		d.Logger.Error("sync: initial snapshot failed",
			slog.String("error", err.Error()),
		)
		return
	}

	events, err := d.Store.Subscribe(ctx)
	if err != nil {
		d.Logger.Error("sync: subscribing to store",
			slog.String("error", err.Error()),
		)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-events:
			if !ok {
				return
			}
			if err := sendSnapshot(ctx, w, flusher, d); err != nil {
				d.Logger.Error("sync: sending snapshot",
					slog.String("error", err.Error()),
				)
				return
			}
		}
	}
}

// sendSnapshot builds a full snapshot from the store and writes it as an SSE event.
func sendSnapshot(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, d *Dependencies) error {
	snap, err := buildSnapshot(ctx, d)
	if err != nil {
		return err
	}

	data, err := json.Marshal(snap)
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
