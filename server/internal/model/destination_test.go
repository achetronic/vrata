package model

import "testing"

func TestResolvedEndpoints_Default(t *testing.T) {
	d := Destination{Host: "10.0.0.1", Port: 8080}
	eps := d.ResolvedEndpoints()
	if len(eps) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(eps))
	}
	if eps[0].Host != "10.0.0.1" || eps[0].Port != 8080 {
		t.Errorf("expected 10.0.0.1:8080, got %s:%d", eps[0].Host, eps[0].Port)
	}
}

func TestResolvedEndpoints_StaticList(t *testing.T) {
	d := Destination{
		Host: "fallback.local",
		Port: 9999,
		Endpoints: []Endpoint{
			{Host: "10.0.0.1", Port: 8080},
			{Host: "10.0.0.2", Port: 8080},
			{Host: "10.0.0.3", Port: 8080},
		},
	}
	eps := d.ResolvedEndpoints()
	if len(eps) != 3 {
		t.Fatalf("expected 3 endpoints, got %d", len(eps))
	}
	for i, ep := range eps {
		if ep.Host != d.Endpoints[i].Host || ep.Port != d.Endpoints[i].Port {
			t.Errorf("endpoint %d mismatch", i)
		}
	}
}

func TestResolvedEndpoints_EmptyList(t *testing.T) {
	d := Destination{
		Host:      "10.0.0.1",
		Port:      8080,
		Endpoints: []Endpoint{},
	}
	eps := d.ResolvedEndpoints()
	if len(eps) != 1 {
		t.Fatalf("expected fallback to Host:Port, got %d endpoints", len(eps))
	}
}
