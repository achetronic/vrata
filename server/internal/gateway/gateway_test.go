package gateway

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/achetronic/vrata/internal/model"
	"github.com/achetronic/vrata/internal/proxy"
	memstore "github.com/achetronic/vrata/internal/store/memory"
)

func TestRebuildBuildsRoutingTable(t *testing.T) {
	st := memstore.New()
	ctx := context.Background()

	st.SaveListener(ctx, model.Listener{ID: "l1", Name: "main", Address: "0.0.0.0", Port: 19999})
	st.SaveRoute(ctx, model.Route{
		ID: "r1", Name: "test",
		Match:          model.MatchRule{PathPrefix: "/"},
		DirectResponse: &model.RouteDirectResponse{Status: 200, Body: "ok"},
	})

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	router := proxy.NewRouter()
	lm := proxy.NewListenerManager(router, logger)
	defer lm.Shutdown()

	gw := New(Dependencies{
		Store:           st,
		Router:          router,
		ListenerManager: lm,
		Logger:          logger,
	})

	if err := gw.Rebuild(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
}

func TestGatewayRunRebuildsOnEvent(t *testing.T) {
	st := memstore.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	router := proxy.NewRouter()
	lm := proxy.NewListenerManager(router, logger)
	defer lm.Shutdown()

	gw := New(Dependencies{
		Store:           st,
		Router:          router,
		ListenerManager: lm,
		Logger:          logger,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- gw.Run(ctx) }()

	time.Sleep(200 * time.Millisecond)

	st.SaveRoute(ctx, model.Route{
		ID: "r1", Name: "test",
		DirectResponse: &model.RouteDirectResponse{Status: 200},
	})

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("gateway.Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("gateway.Run did not return")
	}
}
