// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

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

func TestLeastRequestBalancer_ChoiceCountZero(t *testing.T) {
	lb := NewLeastRequestBalancer(0)
	backends := []model.DestinationRef{
		{DestinationID: "10.0.0.1:8080"},
		{DestinationID: "10.0.0.2:8080"},
	}
	endpoints := makeEndpoints()

	r := httptest.NewRequest("GET", "/", nil)
	ep := lb.Pick(r, backends, endpoints)
	if ep == nil {
		t.Fatal("should pick an endpoint")
	}
	lb.Done(ep.ID)
}

func TestLeastRequestBalancer_ChoiceCountPick(t *testing.T) {
	lb := NewLeastRequestBalancer(1)
	backends := []model.DestinationRef{
		{DestinationID: "10.0.0.1:8080"},
		{DestinationID: "10.0.0.2:8080"},
	}
	endpoints := makeEndpoints()

	r := httptest.NewRequest("GET", "/", nil)
	ep := lb.Pick(r, backends, endpoints)
	if ep == nil {
		t.Fatal("should pick an endpoint with choiceCount=1")
	}
	lb.Done(ep.ID)
}

func TestLeastRequestBalancer_PicksLowest(t *testing.T) {
	lb := NewLeastRequestBalancer(0)
	backends := []model.DestinationRef{
		{DestinationID: "10.0.0.1:8080"},
		{DestinationID: "10.0.0.2:8080"},
	}
	endpoints := makeEndpoints()

	r := httptest.NewRequest("GET", "/", nil)
	lb.Pick(r, backends, endpoints)
	lb.Pick(r, backends, endpoints)
	lb.Pick(r, backends, endpoints)

	lb.Done("10.0.0.1:8080")
	lb.Done("10.0.0.1:8080")
	lb.Done("10.0.0.1:8080")

	ep := lb.Pick(r, backends, endpoints)
	if ep == nil {
		t.Fatal("should pick an endpoint")
	}
}

func TestSampleDests(t *testing.T) {
	dests := []model.DestinationRef{
		{DestinationID: "a"},
		{DestinationID: "b"},
		{DestinationID: "c"},
		{DestinationID: "d"},
	}
	sample := sampleDests(dests, 2)
	if len(sample) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(sample))
	}

	all := sampleDests(dests, 10)
	if len(all) != 4 {
		t.Fatalf("should return all when n > len, got %d", len(all))
	}
}
