package proxy

import (
	"net/http/httptest"
	"testing"

	"github.com/achetronic/vrata/internal/model"
)

func makeEndpoints() map[string]*Endpoint {
	return map[string]*Endpoint{
		"10.0.0.1:8080": {Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 8080}, ID: "10.0.0.1:8080", Healthy: true},
		"10.0.0.2:8080": {Endpoint: model.Endpoint{Host: "10.0.0.2", Port: 8080}, ID: "10.0.0.2:8080", Healthy: true},
	}
}

func makePools() map[string]*DestinationPool {
	return map[string]*DestinationPool{
		"a": {
			Destination: model.Destination{ID: "a", Host: "10.0.0.1", Port: 8080},
			Endpoints: []*Endpoint{
				{Endpoint: model.Endpoint{Host: "10.0.0.1", Port: 8080}, ID: "10.0.0.1:8080", Healthy: true},
			},
		},
		"b": {
			Destination: model.Destination{ID: "b", Host: "10.0.0.2", Port: 8080},
			Endpoints: []*Endpoint{
				{Endpoint: model.Endpoint{Host: "10.0.0.2", Port: 8080}, ID: "10.0.0.2:8080", Healthy: true},
			},
		},
	}
}

func TestWeightedRandomSelection(t *testing.T) {
	backends := []model.DestinationRef{
		{DestinationID: "a", Weight: 50},
		{DestinationID: "b", Weight: 50},
	}
	pools := makePools()

	counts := map[string]int{}
	for i := 0; i < 1000; i++ {
		pool := SelectDestination(backends, pools)
		if pool == nil {
			t.Fatal("SelectDestination returned nil")
		}
		counts[pool.Destination.ID]++
	}

	if counts["a"] == 0 || counts["b"] == 0 {
		t.Errorf("expected both destinations selected, got a=%d b=%d", counts["a"], counts["b"])
	}
}

func TestRoundRobinBalancer(t *testing.T) {
	rr := &RoundRobinBalancer{}
	backends := []model.DestinationRef{
		{DestinationID: "10.0.0.1:8080"},
		{DestinationID: "10.0.0.2:8080"},
	}
	endpoints := makeEndpoints()
	r := httptest.NewRequest("GET", "/", nil)

	first := rr.Pick(r, backends, endpoints)
	second := rr.Pick(r, backends, endpoints)

	if first.ID == second.ID {
		t.Error("round robin should alternate")
	}
}

func TestRingHashConsistency(t *testing.T) {
	rh := NewRingHashBalancer(1024, 8388608)
	backends := []model.DestinationRef{
		{DestinationID: "10.0.0.1:8080"},
		{DestinationID: "10.0.0.2:8080"},
	}
	endpoints := makeEndpoints()
	rh.Build(backends)

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "1.2.3.4:5678"

	first := rh.Pick(r, backends, endpoints)
	second := rh.Pick(r, backends, endpoints)

	if first.ID != second.ID {
		t.Error("ring hash should return same endpoint for same client")
	}
}
