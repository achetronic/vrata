// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package k8s

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/achetronic/vrata/internal/model"
	memstore "github.com/achetronic/vrata/internal/store/memory"
)

func boolPtr(b bool) *bool     { return &b }
func int32Ptr(i int32) *int32  { return &i }

func TestWatcherEndpointSlices(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := fake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "my-svc", Namespace: "default"},
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
		},
		&discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-svc-abc",
				Namespace: "default",
				Labels:    map[string]string{discoveryv1.LabelServiceName: "my-svc"},
			},
			Ports: []discoveryv1.EndpointPort{
				{Port: int32Ptr(8080)},
			},
			Endpoints: []discoveryv1.Endpoint{
				{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: boolPtr(true)}},
				{Addresses: []string{"10.0.0.2"}, Conditions: discoveryv1.EndpointConditions{Ready: boolPtr(true)}},
				{Addresses: []string{"10.0.0.3"}, Conditions: discoveryv1.EndpointConditions{Ready: boolPtr(false)}},
			},
		},
	)

	st := memstore.New()
	st.SaveDestination(ctx, model.Destination{
		ID:   "d1",
		Name: "test",
		Host: "my-svc.default.svc.cluster.local",
		Port: 8080,
		Options: &model.DestinationOptions{
			Discovery: &model.DestinationDiscovery{Type: model.DiscoveryTypeKubernetes},
		},
	})

	var changes atomic.Int32
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	w := New(Dependencies{
		Store:  st,
		Client: client,
		Logger: logger,
	})
	w.SetOnChange(func(_ context.Context) error {
		changes.Add(1)
		return nil
	})

	go w.Run(ctx)

	// Wait for the watcher to pick up endpoints.
	deadline := time.After(3 * time.Second)
	for {
		eps := w.Endpoints()
		if d1, ok := eps["d1"]; ok && len(d1) == 2 {
			for _, ep := range d1 {
				if ep.Port != 8080 {
					t.Errorf("expected port 8080, got %d", ep.Port)
				}
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for endpoints, got: %v", w.Endpoints())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func TestWatcherExternalNameService(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := fake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "ext-svc", Namespace: "default"},
			Spec: corev1.ServiceSpec{
				Type:         corev1.ServiceTypeExternalName,
				ExternalName: "db.example.com",
			},
		},
	)

	st := memstore.New()
	st.SaveDestination(ctx, model.Destination{
		ID:   "d1",
		Name: "external-db",
		Host: "ext-svc.default.svc.cluster.local",
		Port: 5432,
		Options: &model.DestinationOptions{
			Discovery: &model.DestinationDiscovery{Type: model.DiscoveryTypeKubernetes},
		},
	})

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	w := New(Dependencies{
		Store:  st,
		Client: client,
		Logger: logger,
	})
	w.SetOnChange(func(_ context.Context) error { return nil })

	go w.Run(ctx)

	deadline := time.After(3 * time.Second)
	for {
		eps := w.Endpoints()
		if d1, ok := eps["d1"]; ok && len(d1) == 1 {
			if d1[0].Host != "db.example.com" {
				t.Errorf("expected address db.example.com, got %s", d1[0].Host)
			}
			if d1[0].Port != 5432 {
				t.Errorf("expected port 5432, got %d", d1[0].Port)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for ExternalName endpoint, got: %v", w.Endpoints())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func TestWatcherNonEDSDestinationIgnored(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	client := fake.NewSimpleClientset()

	st := memstore.New()
	st.SaveDestination(ctx, model.Destination{
		ID:   "d1",
		Name: "plain",
		Host: "10.0.0.1",
		Port: 80,
	})

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	w := New(Dependencies{
		Store:  st,
		Client: client,
		Logger: logger,
	})
	w.SetOnChange(func(_ context.Context) error { return nil })

	go w.Run(ctx)

	time.Sleep(500 * time.Millisecond)

	eps := w.Endpoints()
	if len(eps) != 0 {
		t.Errorf("expected no endpoints for non-EDS destination, got %v", eps)
	}
}

func TestWatcherDestinationRemoved(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := fake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "ext-svc", Namespace: "default"},
			Spec: corev1.ServiceSpec{
				Type:         corev1.ServiceTypeExternalName,
				ExternalName: "db.example.com",
			},
		},
	)

	st := memstore.New()
	st.SaveDestination(ctx, model.Destination{
		ID:   "d1",
		Name: "external-db",
		Host: "ext-svc.default.svc.cluster.local",
		Port: 5432,
		Options: &model.DestinationOptions{
			Discovery: &model.DestinationDiscovery{Type: model.DiscoveryTypeKubernetes},
		},
	})

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	w := New(Dependencies{
		Store:  st,
		Client: client,
		Logger: logger,
	})
	w.SetOnChange(func(_ context.Context) error { return nil })

	go w.Run(ctx)

	// Wait for endpoint to appear.
	deadline := time.After(3 * time.Second)
	for {
		if d1, ok := w.Endpoints()["d1"]; ok && len(d1) == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for endpoint")
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Remove the destination.
	st.DeleteDestination(ctx, "d1")

	// Wait for endpoints to be cleared.
	deadline = time.After(3 * time.Second)
	for {
		eps := w.Endpoints()
		if _, ok := eps["d1"]; !ok {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for endpoint removal, got: %v", w.Endpoints())
		case <-time.After(50 * time.Millisecond):
		}
	}
}
