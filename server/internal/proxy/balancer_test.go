package proxy

import (
	"net/http/httptest"
	"testing"

	"github.com/achetronic/rutoso/internal/model"
)

func makeUpstreams() map[string]*Upstream {
	return map[string]*Upstream{
		"a": {Destination: model.Destination{ID: "a"}, Healthy: true},
		"b": {Destination: model.Destination{ID: "b"}, Healthy: true},
	}
}

func TestWeightedRandomSelection(t *testing.T) {
	backends := []model.DestinationRef{
		{DestinationID: "a", Weight: 50},
		{DestinationID: "b", Weight: 50},
	}
	upstreams := makeUpstreams()

	counts := map[string]int{}
	for i := 0; i < 1000; i++ {
		u := SelectDestination(backends, upstreams)
		if u == nil {
			t.Fatal("SelectDestination returned nil")
		}
		counts[u.Destination.ID]++
	}

	// Both should be selected at least sometimes.
	if counts["a"] == 0 || counts["b"] == 0 {
		t.Errorf("expected both backends selected, got a=%d b=%d", counts["a"], counts["b"])
	}
}

func TestRoundRobinBalancer(t *testing.T) {
	rr := &RoundRobinBalancer{}
	backends := []model.DestinationRef{
		{DestinationID: "a"},
		{DestinationID: "b"},
	}
	upstreams := makeUpstreams()
	r := httptest.NewRequest("GET", "/", nil)

	first := rr.Pick(r, backends, upstreams)
	second := rr.Pick(r, backends, upstreams)

	if first.Destination.ID == second.Destination.ID {
		t.Error("round robin should alternate")
	}
}

func TestRingHashConsistency(t *testing.T) {
	rh := NewRingHashBalancer(1024, 8388608)
	backends := []model.DestinationRef{
		{DestinationID: "a"},
		{DestinationID: "b"},
	}
	upstreams := makeUpstreams()
	rh.Build(backends)

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "1.2.3.4:5678"

	first := rh.Pick(r, backends, upstreams)
	second := rh.Pick(r, backends, upstreams)

	if first.Destination.ID != second.Destination.ID {
		t.Error("ring hash should return same backend for same client")
	}
}
