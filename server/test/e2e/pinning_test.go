package e2e

import (
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

// proxyGetWithJar sends a GET through the proxy using a cookie jar,
// so session cookies persist across requests.
func proxyGetWithJar(t *testing.T, jar http.CookieJar, path string) (int, string) {
	t.Helper()
	client := &http.Client{
		Timeout:       5 * time.Second,
		Jar:           jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get(proxyURL + path)
	if err != nil {
		t.Fatalf("proxy GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	data := make([]byte, 1024)
	n, _ := resp.Body.Read(data)
	return resp.StatusCode, string(data[:n])
}

func TestE2E_Proxy_DestinationPinning(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-pin-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-pin-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-pin",
		"match": map[string]any{"pathPrefix": "/e2e-pin"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 50},
				{"destinationId": destB, "weight": 50},
			},
			"destinationPinning": map[string]any{
				"cookieName": "_rutoso_pin",
				"ttl":        "1h",
			},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	// Each "user" (jar) should be pinned to the same destination across requests.
	for user := 0; user < 10; user++ {
		jar, _ := cookiejar.New(nil)
		_, first := proxyGetWithJar(t, jar, "/e2e-pin")
		if first != "A" && first != "B" {
			t.Fatalf("user %d: unexpected response %q", user, first)
		}
		for req := 0; req < 20; req++ {
			_, body := proxyGetWithJar(t, jar, "/e2e-pin")
			if body != first {
				t.Fatalf("user %d request %d: pinning broken, first=%s got=%s", user, req, first, body)
			}
		}
	}
}

func TestE2E_Proxy_DestinationPinningWeightRespected(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-pinw-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-pinw-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-pinw",
		"match": map[string]any{"pathPrefix": "/e2e-pinw"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 90},
				{"destinationId": destB, "weight": 10},
			},
			"destinationPinning": map[string]any{
				"cookieName": "_rutoso_pinw",
				"ttl":        "1h",
			},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	// 1000 unique users, each gets pinned on first request.
	// Distribution should respect 90/10 weights.
	counts := map[string]int{}
	for i := 0; i < 1000; i++ {
		jar, _ := cookiejar.New(nil)
		_, body := proxyGetWithJar(t, jar, "/e2e-pinw")
		counts[body]++
	}

	aPct := float64(counts["A"]) / 1000.0
	if aPct < 0.80 || aPct > 0.97 {
		t.Errorf("expected ~90%% A, got %.1f%% (A=%d B=%d)", aPct*100, counts["A"], counts["B"])
	}
}

func TestE2E_Proxy_DestinationPinningDestinationRemoved(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-pinr-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-pinr-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-pinr",
		"match": map[string]any{"pathPrefix": "/e2e-pinr"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 50},
				{"destinationId": destB, "weight": 50},
			},
			"destinationPinning": map[string]any{"cookieName": "_rutoso_pinr", "ttl": "1h"},
		},
	})
	routeID := id(route)
	defer apiDelete(t, "/routes/"+routeID)

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	// Pin a user to one destination.
	jar, _ := cookiejar.New(nil)
	_, first := proxyGetWithJar(t, jar, "/e2e-pinr")

	// Remove that destination from the backends, keep only the other.
	remaining := destB
	if first == "B" {
		remaining = destA
	}
	apiPut(t, "/routes/"+routeID, map[string]any{
		"name":  "e2e-pinr",
		"match": map[string]any{"pathPrefix": "/e2e-pinr"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": remaining, "weight": 100},
			},
			"destinationPinning": map[string]any{"cookieName": "_rutoso_pinr", "ttl": "1h"},
		},
	})
	snapID2 := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID2)

	// Same user should now go to the remaining destination.
	_, body := proxyGetWithJar(t, jar, "/e2e-pinr")
	other := "B"
	if first == "B" {
		other = "A"
	}
	if body != other {
		t.Errorf("after removal: expected %s, got %s", other, body)
	}
}

func TestE2E_Proxy_DestinationPinningMultipleRoutes(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-pinm-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-pinm-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	pinCfg := map[string]any{"cookieName": "_rutoso_pinm", "ttl": "1h"}

	_, route1 := apiPost(t, "/routes", map[string]any{
		"name": "e2e-pinm1", "match": map[string]any{"pathPrefix": "/e2e-pinm1"},
		"forward": map[string]any{
			"destinations":           []map[string]any{{"destinationId": destA, "weight": 50}, {"destinationId": destB, "weight": 50}},
			"destinationPinning": pinCfg,
		},
	})
	defer apiDelete(t, "/routes/"+id(route1))

	_, route2 := apiPost(t, "/routes", map[string]any{
		"name": "e2e-pinm2", "match": map[string]any{"pathPrefix": "/e2e-pinm2"},
		"forward": map[string]any{
			"destinations":           []map[string]any{{"destinationId": destA, "weight": 50}, {"destinationId": destB, "weight": 50}},
			"destinationPinning": pinCfg,
		},
	})
	defer apiDelete(t, "/routes/"+id(route2))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	// Same user (same jar = same cookie) should be pinned independently per route.
	// The hash includes routeID, so different routes can land on different destinations.
	jar, _ := cookiejar.New(nil)

	_, body1 := proxyGetWithJar(t, jar, "/e2e-pinm1")
	_, body2 := proxyGetWithJar(t, jar, "/e2e-pinm2")

	// Verify each is individually sticky.
	for i := 0; i < 20; i++ {
		_, b1 := proxyGetWithJar(t, jar, "/e2e-pinm1")
		_, b2 := proxyGetWithJar(t, jar, "/e2e-pinm2")
		if b1 != body1 {
			t.Fatalf("route1 not sticky: expected %s, got %s on request %d", body1, b1, i)
		}
		if b2 != body2 {
			t.Fatalf("route2 not sticky: expected %s, got %s on request %d", body2, b2, i)
		}
	}

	// Log which destination each route got — they may or may not differ
	// depending on the hash, but both must be individually stable.
	t.Logf("route1=%s route2=%s (both stable across 20 requests)", body1, body2)
}

func TestE2E_Proxy_NoPinningStillRandom(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-nopin-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-nopin-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-nopin",
		"match": map[string]any{"pathPrefix": "/e2e-nopin"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 50},
				{"destinationId": destB, "weight": 50},
			},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	// Without pinning, same client should hit both destinations over many requests.
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		_, _, body := proxyGet(t, "/e2e-nopin", nil)
		seen[strings.TrimSpace(body)] = true
		if len(seen) == 2 {
			break
		}
	}
	if len(seen) < 2 {
		t.Errorf("without pinning expected traffic to both destinations, only saw %v", seen)
	}
}
