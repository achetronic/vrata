// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package vrata

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListRoutes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/routes" || r.Method != http.MethodGet {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode([]Route{
			{ID: "r1", Name: "k8s:default/test/rule-0/match-0"},
			{ID: "r2", Name: "manual-route"},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	routes, err := c.ListRoutes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}
}

func TestCreateRoute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var route Route
		json.NewDecoder(r.Body).Decode(&route)
		route.ID = "new-id"
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(route)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	created, err := c.CreateRoute(context.Background(), Route{Name: "k8s:test"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "new-id" {
		t.Errorf("expected id new-id, got %s", created.ID)
	}
}

func TestDeleteRoute(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/routes/r1" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		called = true
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if err := c.DeleteRoute(context.Background(), "r1"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("delete was not called")
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.ListRoutes(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 404 {
		t.Errorf("expected 404, got %d", apiErr.StatusCode)
	}
}

func TestCreateAndActivateSnapshot(t *testing.T) {
	step := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch step {
		case 0:
			if r.Method != http.MethodPost || r.URL.Path != "/api/v1/snapshots" {
				t.Errorf("step 0: unexpected %s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(Snapshot{ID: "snap-1", Name: "test"})
		case 1:
			if r.Method != http.MethodPost || r.URL.Path != "/api/v1/snapshots/snap-1/activate" {
				t.Errorf("step 1: unexpected %s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(200)
		}
		step++
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	snap, err := c.CreateSnapshot(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if snap.ID != "snap-1" {
		t.Errorf("expected snap-1, got %s", snap.ID)
	}
	if err := c.ActivateSnapshot(context.Background(), snap.ID); err != nil {
		t.Fatal(err)
	}
}

func TestOwned(t *testing.T) {
	routes := []Route{
		{Name: "k8s:default/test/rule-0/match-0"},
		{Name: "manual-route"},
		{Name: "k8s:prod/api/rule-1/match-0"},
	}
	owned := Owned(routes)
	if len(owned) != 2 {
		t.Errorf("expected 2 owned, got %d", len(owned))
	}
}
