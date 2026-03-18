package e2e

import (
	"fmt"
	"math"
	"net/http"
	"net/http/cookiejar"
	"sync"
	"testing"
)

// ─── Case 1: Weighted Random × Direct (no stickiness) ─────────────────────

// TestE2E_Proxy_Case1_WeightedRandom_Direct verifies weighted random destination
// selection without any stickiness. Each request is independently routed.
// 5000 requests at 70/30 weights must produce a distribution within 5% tolerance.
func TestE2E_Proxy_Case1_WeightedRandom_Direct(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-c1-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-c1-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-c1",
		"match": map[string]any{"pathPrefix": "/e2e-c1"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 70},
				{"destinationId": destB, "weight": 30},
			},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	const total = 5000
	countA, countB := 0, 0
	for i := 0; i < total; i++ {
		_, _, body := proxyGet(t, "/e2e-c1", nil)
		switch body {
		case "A":
			countA++
		case "B":
			countB++
		}
	}
	assertBalancingWeight(t, "A", countA, total, 0.70, 0.05)
	assertBalancingWeight(t, "B", countB, total, 0.30, 0.05)
	t.Logf("distribution: A=%d (%.1f%%) B=%d (%.1f%%)", countA, float64(countA)*100/float64(total), countB, float64(countB)*100/float64(total))
}

// ─── Case 2: Weighted Random × RingHash by header ──────────────────────────

// TestE2E_Proxy_Case2_WeightedRandom_RingHashHeader verifies that level-1 is
// weighted random (no stickiness) and level-2 uses RING_HASH with header hash
// policy. The same X-User-ID header should consistently route to the same
// destination due to the ring hash.
func TestE2E_Proxy_Case2_WeightedRandom_RingHashHeader(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestinationWithEndpointBalancing(t, "e2e-c2-a", upA.host(), upA.port(), map[string]any{
		"algorithm": "RING_HASH",
		"ringHash": map[string]any{
			"hashPolicy": []map[string]any{
				{"header": map[string]any{"name": "X-User-ID"}},
			},
		},
	})
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestinationWithEndpointBalancing(t, "e2e-c2-b", upB.host(), upB.port(), map[string]any{
		"algorithm": "RING_HASH",
		"ringHash": map[string]any{
			"hashPolicy": []map[string]any{
				{"header": map[string]any{"name": "X-User-ID"}},
			},
		},
	})
	defer apiDelete(t, "/destinations/"+destB)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-c2",
		"match": map[string]any{"pathPrefix": "/e2e-c2"},
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

	// 100 unique users, each sends 50 requests with the same X-User-ID header.
	// Each user should always land on the same destination (hash consistency).
	const users = 100
	const reqsPerUser = 50
	broken := 0
	for u := 0; u < users; u++ {
		userID := fmt.Sprintf("user-%d", u)
		headers := map[string]string{"X-User-ID": userID}
		_, _, first := proxyGet(t, "/e2e-c2", headers)
		for r := 1; r < reqsPerUser; r++ {
			_, _, body := proxyGet(t, "/e2e-c2", headers)
			if body != first {
				broken++
				break
			}
		}
	}
	if broken > 0 {
		t.Errorf("ring hash consistency broken: %d/%d users got different destinations", broken, users)
	}
	t.Logf("consistent: %d/%d users always got same destination (%d reqs each)", users-broken, users, reqsPerUser)
}

// ─── Case 3: Weighted Consistent Hash (pinned destinations) ────────────────

// TestE2E_Proxy_Case3_PinnedDestination_5kRequests verifies that destination
// pinning via WEIGHTED_CONSISTENT_HASH keeps each client pinned to the same
// destination across 5000+ total requests while changing weights 3 times.
func TestE2E_Proxy_Case3_PinnedDestination_5kRequests(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-c3-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-c3-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	routeBody := func(wA, wB int) map[string]any {
		return map[string]any{
			"name":  "e2e-c3",
			"match": map[string]any{"pathPrefix": "/e2e-c3"},
			"forward": map[string]any{
				"destinations": []map[string]any{
					{"destinationId": destA, "weight": wA},
					{"destinationId": destB, "weight": wB},
				},
				"destinationBalancing": destBalancing("_vrata_c3", "1h"),
			},
		}
	}

	_, route := apiPost(t, "/routes", routeBody(80, 20))
	routeID := id(route)
	defer apiDelete(t, "/routes/"+routeID)

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	// Pin 200 users at 80/20.
	const users = 200
	type userState struct {
		jar  http.CookieJar
		dest string
	}
	pinned := make([]userState, users)
	for i := range pinned {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Timeout: 5e9, Jar: jar}
		resp, err := c.Get(proxyURL + fmt.Sprintf("/e2e-c3?u=%d", i))
		if err != nil {
			t.Fatalf("user %d warmup: %v", i, err)
		}
		buf := make([]byte, 4)
		n, _ := resp.Body.Read(buf)
		resp.Body.Close()
		pinned[i] = userState{jar: jar, dest: string(buf[:n])}
	}

	// 3 weight epochs: 50/50, 30/70, 10/90
	epochs := []struct{ wA, wB int }{{50, 50}, {30, 70}, {10, 90}}
	totalRequests := 0
	for epoch, e := range epochs {
		apiPut(t, "/routes/"+routeID, routeBody(e.wA, e.wB))
		snap := activateSnapshot(t)
		defer apiDelete(t, "/snapshots/"+snap)

		broken := 0
		for i, u := range pinned {
			c := &http.Client{Timeout: 5e9, Jar: u.jar}
			for r := 0; r < 25; r++ {
				totalRequests++
				resp, err := c.Get(proxyURL + fmt.Sprintf("/e2e-c3?u=%d&e=%d&r=%d", i, epoch, r))
				if err != nil {
					t.Fatalf("user %d epoch %d req %d: %v", i, epoch, r, err)
				}
				buf := make([]byte, 4)
				n, _ := resp.Body.Read(buf)
				resp.Body.Close()
				if string(buf[:n]) != u.dest {
					broken++
					break
				}
			}
		}
		if broken > 0 {
			t.Errorf("epoch %d (%d/%d): %d/%d users switched destination", epoch+1, e.wA, e.wB, broken, users)
		}

		// Fresh users should reflect new weights.
		freshA, freshB := 0, 0
		for i := 0; i < 500; i++ {
			jar, _ := cookiejar.New(nil)
			c := &http.Client{Timeout: 5e9, Jar: jar}
			resp, err := c.Get(proxyURL + fmt.Sprintf("/e2e-c3?fresh=%d&e=%d", i, epoch))
			if err != nil {
				t.Fatalf("fresh user %d: %v", i, err)
			}
			buf := make([]byte, 4)
			n, _ := resp.Body.Read(buf)
			resp.Body.Close()
			switch string(buf[:n]) {
			case "A":
				freshA++
			case "B":
				freshB++
			}
		}
		ratioA := float64(e.wA) / float64(e.wA+e.wB)
		ratioB := float64(e.wB) / float64(e.wA+e.wB)
		assertBalancingWeight(t, fmt.Sprintf("epoch%d A", epoch+1), freshA, 500, ratioA, 0.08)
		assertBalancingWeight(t, fmt.Sprintf("epoch%d B", epoch+1), freshB, 500, ratioB, 0.08)
	}
	t.Logf("total requests: %d (pinned) + %d (fresh) = %d", totalRequests, 3*500, totalRequests+3*500)
}

// ─── Case 4: Combined L1 + L2 stickiness with concurrent weight changes ───

// TestE2E_Proxy_Case4_CombinedL1L2_Concurrent verifies both levels together:
// - Level 1: destination pinning via WEIGHTED_CONSISTENT_HASH (cookie)
// - Level 2: endpoint stickiness via RING_HASH with header hash policy
// 50 users × 100 requests × 2 epochs = 10000+ requests total.
func TestE2E_Proxy_Case4_CombinedL1L2_Concurrent(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestinationWithEndpointBalancing(t, "e2e-c4-a", upA.host(), upA.port(), map[string]any{
		"algorithm": "RING_HASH",
		"ringHash": map[string]any{
			"hashPolicy": []map[string]any{
				{"header": map[string]any{"name": "X-Session"}},
			},
		},
	})
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestinationWithEndpointBalancing(t, "e2e-c4-b", upB.host(), upB.port(), map[string]any{
		"algorithm": "RING_HASH",
		"ringHash": map[string]any{
			"hashPolicy": []map[string]any{
				{"header": map[string]any{"name": "X-Session"}},
			},
		},
	})
	defer apiDelete(t, "/destinations/"+destB)

	routeBody := func(wA, wB int) map[string]any {
		return map[string]any{
			"name":  "e2e-c4",
			"match": map[string]any{"pathPrefix": "/e2e-c4"},
			"forward": map[string]any{
				"destinations": []map[string]any{
					{"destinationId": destA, "weight": wA},
					{"destinationId": destB, "weight": wB},
				},
				"destinationBalancing": destBalancing("_vrata_c4", "1h"),
			},
		}
	}

	_, route := apiPost(t, "/routes", routeBody(70, 30))
	routeID := id(route)
	defer apiDelete(t, "/routes/"+routeID)
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	// Pin 50 users.
	const users = 50
	type user struct {
		jar     http.CookieJar
		dest    string
		session string
	}
	cohort := make([]user, users)
	for i := range cohort {
		jar, _ := cookiejar.New(nil)
		session := fmt.Sprintf("session-%d", i)
		c := &http.Client{Timeout: 5e9, Jar: jar}
		req, _ := http.NewRequest("GET", proxyURL+fmt.Sprintf("/e2e-c4?u=%d", i), nil)
		req.Header.Set("X-Session", session)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("user %d warmup: %v", i, err)
		}
		buf := make([]byte, 4)
		n, _ := resp.Body.Read(buf)
		resp.Body.Close()
		cohort[i] = user{jar: jar, dest: string(buf[:n]), session: session}
	}

	// 2 epochs: 30/70, 20/80
	epochs := []struct{ wA, wB int }{{30, 70}, {20, 80}}
	for epoch, e := range epochs {
		apiPut(t, "/routes/"+routeID, routeBody(e.wA, e.wB))
		snap := activateSnapshot(t)
		defer apiDelete(t, "/snapshots/"+snap)

		var wg sync.WaitGroup
		conflicts := make([]string, users)
		for i, u := range cohort {
			wg.Add(1)
			go func(idx int, u user) {
				defer wg.Done()
				c := &http.Client{Timeout: 5e9, Jar: u.jar}
				for r := 0; r < 100; r++ {
					req, _ := http.NewRequest("GET", proxyURL+fmt.Sprintf("/e2e-c4?u=%d&e=%d&r=%d", idx, epoch, r), nil)
					req.Header.Set("X-Session", u.session)
					resp, err := c.Do(req)
					if err != nil {
						conflicts[idx] = fmt.Sprintf("req error: %v", err)
						return
					}
					buf := make([]byte, 4)
					n, _ := resp.Body.Read(buf)
					resp.Body.Close()
					if string(buf[:n]) != u.dest {
						conflicts[idx] = fmt.Sprintf("epoch %d req %d: expected %s got %s", epoch+1, r, u.dest, string(buf[:n]))
						return
					}
				}
			}(i, u)
		}
		wg.Wait()
		for i, msg := range conflicts {
			if msg != "" {
				t.Errorf("user %d: %s", i, msg)
			}
		}
	}
	t.Logf("total: %d users × 100 reqs × 2 epochs = %d requests", users, users*100*2)
}

// ─── Case 5: Multiple routes same cookie (isolation test) ──────────────────

// TestE2E_Proxy_Case5_MultiRoute_CookieIsolation verifies that two routes
// sharing the same session cookie name pin independently (routeID in hash).
// 5000+ requests across 2 routes × 100 users.
func TestE2E_Proxy_Case5_MultiRoute_CookieIsolation(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-c5-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-c5-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	sharedBalancing := destBalancing("_vrata_shared", "1h")

	_, route1 := apiPost(t, "/routes", map[string]any{
		"name": "e2e-c5r1", "match": map[string]any{"pathPrefix": "/e2e-c5r1"},
		"forward": map[string]any{
			"destinations":        []map[string]any{{"destinationId": destA, "weight": 50}, {"destinationId": destB, "weight": 50}},
			"destinationBalancing": sharedBalancing,
		},
	})
	defer apiDelete(t, "/routes/"+id(route1))

	_, route2 := apiPost(t, "/routes", map[string]any{
		"name": "e2e-c5r2", "match": map[string]any{"pathPrefix": "/e2e-c5r2"},
		"forward": map[string]any{
			"destinations":        []map[string]any{{"destinationId": destA, "weight": 50}, {"destinationId": destB, "weight": 50}},
			"destinationBalancing": sharedBalancing,
		},
	})
	defer apiDelete(t, "/routes/"+id(route2))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	// 100 users, each sends 25 requests to each route. Both routes must be
	// individually sticky, but they may pin to different destinations
	// (routeID differs in hash).
	const users = 100
	broken := 0
	for i := 0; i < users; i++ {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Timeout: 5e9, Jar: jar}

		resp1, _ := c.Get(proxyURL + fmt.Sprintf("/e2e-c5r1?u=%d", i))
		buf := make([]byte, 4)
		n, _ := resp1.Body.Read(buf)
		resp1.Body.Close()
		dest1 := string(buf[:n])

		resp2, _ := c.Get(proxyURL + fmt.Sprintf("/e2e-c5r2?u=%d", i))
		buf2 := make([]byte, 4)
		n2, _ := resp2.Body.Read(buf2)
		resp2.Body.Close()
		dest2 := string(buf2[:n2])

		for r := 0; r < 25; r++ {
			r1, _ := c.Get(proxyURL + fmt.Sprintf("/e2e-c5r1?u=%d&r=%d", i, r))
			b1 := make([]byte, 4)
			n1, _ := r1.Body.Read(b1)
			r1.Body.Close()
			if string(b1[:n1]) != dest1 {
				broken++
				break
			}

			r2, _ := c.Get(proxyURL + fmt.Sprintf("/e2e-c5r2?u=%d&r=%d", i, r))
			b2 := make([]byte, 4)
			n2, _ := r2.Body.Read(b2)
			r2.Body.Close()
			if string(b2[:n2]) != dest2 {
				broken++
				break
			}
		}
	}
	if broken > 0 {
		t.Errorf("cookie isolation broken: %d/%d users had stickiness failure", broken, users)
	}
	t.Logf("total: %d users × 50 reqs (25 per route) = %d requests", users, users*50)
}

// ─── Case 6: SourceIP hash policy ──────────────────────────────────────────

// TestE2E_Proxy_Case6_SourceIPHash verifies that sourceIP hash policy produces
// consistent routing for the same client. Since all e2e requests come from
// 127.0.0.1, every request should land on the same destination.
func TestE2E_Proxy_Case6_SourceIPHash(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestinationWithEndpointBalancing(t, "e2e-c6-a", upA.host(), upA.port(), map[string]any{
		"algorithm": "RING_HASH",
		"ringHash": map[string]any{
			"hashPolicy": []map[string]any{
				{"sourceIP": map[string]any{"enabled": true}},
			},
		},
	})
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestinationWithEndpointBalancing(t, "e2e-c6-b", upB.host(), upB.port(), map[string]any{
		"algorithm": "RING_HASH",
		"ringHash": map[string]any{
			"hashPolicy": []map[string]any{
				{"sourceIP": map[string]any{"enabled": true}},
			},
		},
	})
	defer apiDelete(t, "/destinations/"+destB)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-c6",
		"match": map[string]any{"pathPrefix": "/e2e-c6"},
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

	// All requests from same IP should go to same destination.
	const total = 5000
	_, _, first := proxyGet(t, "/e2e-c6", nil)
	broken := 0
	for i := 1; i < total; i++ {
		_, _, body := proxyGet(t, "/e2e-c6", nil)
		if body != first {
			broken++
		}
	}
	if broken > 0 {
		t.Errorf("sourceIP hash inconsistent: %d/%d requests went to different destination", broken, total)
	}
	t.Logf("sourceIP hash: all %d requests went to %s", total, first)
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func createDestinationWithEndpointBalancing(t *testing.T, name, host string, port int, eb map[string]any) string {
	t.Helper()
	_, d := apiPost(t, "/destinations", map[string]any{
		"name": name, "host": host, "port": port,
		"options": map[string]any{
			"endpointBalancing": eb,
		},
	})
	if d["id"] == nil {
		t.Fatalf("create destination %s failed: %v", name, d)
	}
	return id(d)
}

func assertBalancingWeight(t *testing.T, label string, got, total int, expectedRatio, tolerance float64) {
	t.Helper()
	actual := float64(got) / float64(total)
	if math.Abs(actual-expectedRatio) > tolerance {
		t.Errorf("%s: expected ~%.0f%% got %.1f%% (%d/%d)",
			label, expectedRatio*100, actual*100, got, total)
	}
}
