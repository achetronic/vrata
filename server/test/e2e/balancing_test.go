package e2e

import (
	"fmt"
	"math"
	"net/http"
	"net/http/cookiejar"
	"sync"
	"sync/atomic"
	"testing"
)

// ─── WEIGHTED_RANDOM ───────────────────────────────────────────────────────

// TestE2E_Proxy_WR_Distribution verifies weighted random destination selection
// distributes traffic proportionally to weights across 5000 requests.
func TestE2E_Proxy_WR_Distribution(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-wr-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-wr-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-wr",
		"match": map[string]any{"pathPrefix": "/e2e-wr"},
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
		_, _, body := proxyGet(t, "/e2e-wr", nil)
		switch body {
		case "A":
			countA++
		case "B":
			countB++
		}
	}
	assertBW(t, "A", countA, total, 0.70, 0.05)
	assertBW(t, "B", countB, total, 0.30, 0.05)
	t.Logf("A=%d (%.1f%%) B=%d (%.1f%%)", countA, pct(countA, total), countB, pct(countB, total))
}

// TestE2E_Proxy_WR_NoStickiness verifies that the same client (cookie jar)
// hits both destinations over 5000 requests (no pinning).
func TestE2E_Proxy_WR_NoStickiness(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-wrns-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-wrns-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-wrns",
		"match": map[string]any{"pathPrefix": "/e2e-wrns"},
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

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Timeout: 5e9, Jar: jar}
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		resp, err := c.Get(proxyURL + "/e2e-wrns")
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		buf := make([]byte, 4)
		n, _ := resp.Body.Read(buf)
		resp.Body.Close()
		seen[string(buf[:n])] = true
		if len(seen) == 2 {
			break
		}
	}
	if len(seen) < 2 {
		t.Errorf("expected traffic to both destinations, only saw %v", seen)
	}
	t.Logf("saw destinations: %v", seen)
}

// TestE2E_Proxy_WR_WeightChange verifies distribution changes when weights change.
func TestE2E_Proxy_WR_WeightChange(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-wrwc-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-wrwc-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	routeBody := func(wA, wB int) map[string]any {
		return map[string]any{
			"name":  "e2e-wrwc",
			"match": map[string]any{"pathPrefix": "/e2e-wrwc"},
			"forward": map[string]any{
				"destinations": []map[string]any{
					{"destinationId": destA, "weight": wA},
					{"destinationId": destB, "weight": wB},
				},
			},
		}
	}

	_, route := apiPost(t, "/routes", routeBody(80, 20))
	routeID := id(route)
	defer apiDelete(t, "/routes/"+routeID)
	snap1 := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap1)

	a1, b1 := sendN(t, "/e2e-wrwc", 5000)
	assertBW(t, "round1 A", a1, 5000, 0.80, 0.05)
	t.Logf("round1: A=%d (%.1f%%) B=%d (%.1f%%)", a1, pct(a1, 5000), b1, pct(b1, 5000))

	apiPut(t, "/routes/"+routeID, routeBody(20, 80))
	snap2 := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap2)

	a2, b2 := sendN(t, "/e2e-wrwc", 5000)
	assertBW(t, "round2 A", a2, 5000, 0.20, 0.05)
	t.Logf("round2: A=%d (%.1f%%) B=%d (%.1f%%)", a2, pct(a2, 5000), b2, pct(b2, 5000))
}

// ─── WEIGHTED_CONSISTENT_HASH ──────────────────────────────────────────────

// TestE2E_Proxy_WCH_Stickiness verifies that the same client always goes to
// the same destination across 5000 requests without weight changes.
func TestE2E_Proxy_WCH_Stickiness(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-wch-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-wch-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-wch",
		"match": map[string]any{"pathPrefix": "/e2e-wch"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 60},
				{"destinationId": destB, "weight": 40},
			},
			"destinationBalancing": destBalancing("_vrata_wch", "1h"),
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	const users = 100
	const reqsPerUser = 50
	broken := 0
	for u := 0; u < users; u++ {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Timeout: 5e9, Jar: jar}
		first := clientGet(t, c, "/e2e-wch")
		for r := 1; r < reqsPerUser; r++ {
			if clientGet(t, c, "/e2e-wch") != first {
				broken++
				break
			}
		}
	}
	if broken > 0 {
		t.Errorf("stickiness broken: %d/%d users switched destination", broken, users)
	}
	t.Logf("sticky: %d/%d users stable (%d reqs each, total %d)", users-broken, users, reqsPerUser, users*reqsPerUser)
}

// TestE2E_Proxy_WCH_WeightDistribution verifies initial distribution matches weights.
func TestE2E_Proxy_WCH_WeightDistribution(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-wchd-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-wchd-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-wchd",
		"match": map[string]any{"pathPrefix": "/e2e-wchd"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 75},
				{"destinationId": destB, "weight": 25},
			},
			"destinationBalancing": destBalancing("_vrata_wchd", "1h"),
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	countA, countB := 0, 0
	for i := 0; i < 5000; i++ {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Timeout: 5e9, Jar: jar}
		switch clientGet(t, c, "/e2e-wchd") {
		case "A":
			countA++
		case "B":
			countB++
		}
	}
	assertBW(t, "A", countA, 5000, 0.75, 0.08)
	assertBW(t, "B", countB, 5000, 0.25, 0.08)
	t.Logf("A=%d (%.1f%%) B=%d (%.1f%%)", countA, pct(countA, 5000), countB, pct(countB, 5000))
}

// TestE2E_Proxy_WCH_WeightChange verifies that weight changes cause minimal
// disruption (proportional to the weight delta, not 100%).
func TestE2E_Proxy_WCH_WeightChange(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-wchw-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-wchw-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	routeBody := func(wA, wB int) map[string]any {
		return map[string]any{
			"name":  "e2e-wchw",
			"match": map[string]any{"pathPrefix": "/e2e-wchw"},
			"forward": map[string]any{
				"destinations": []map[string]any{
					{"destinationId": destA, "weight": wA},
					{"destinationId": destB, "weight": wB},
				},
				"destinationBalancing": destBalancing("_vrata_wchw", "1h"),
			},
		}
	}

	_, route := apiPost(t, "/routes", routeBody(80, 20))
	routeID := id(route)
	defer apiDelete(t, "/routes/"+routeID)
	snap1 := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap1)

	// Pin 500 users.
	type pinned struct {
		jar  http.CookieJar
		dest string
	}
	users := make([]pinned, 500)
	for i := range users {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Timeout: 5e9, Jar: jar}
		users[i] = pinned{jar: jar, dest: clientGet(t, c, "/e2e-wchw")}
	}

	// Change to 60/40.
	apiPut(t, "/routes/"+routeID, routeBody(60, 40))
	snap2 := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap2)

	moved := 0
	for _, u := range users {
		c := &http.Client{Timeout: 5e9, Jar: u.jar}
		if clientGet(t, c, "/e2e-wchw") != u.dest {
			moved++
		}
	}

	// Weight delta is 20pp (80→60). We expect disruption < 50%
	// (consistent hash moves proportional to delta, not all).
	movePct := float64(moved) / float64(len(users)) * 100
	t.Logf("weight 80/20 → 60/40: %d/%d users moved (%.1f%%)", moved, len(users), movePct)
	if movePct > 50 {
		t.Errorf("disruption too high: %.1f%% > 50%%", movePct)
	}
}

// TestE2E_Proxy_WCH_MultiRoute_Isolation verifies that two routes with the
// same cookie name pin independently (routeID in hash isolates them).
func TestE2E_Proxy_WCH_MultiRoute_Isolation(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-wchmr-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-wchmr-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	bal := destBalancing("_vrata_shared", "1h")

	_, r1 := apiPost(t, "/routes", map[string]any{
		"name": "e2e-wchmr1", "match": map[string]any{"pathPrefix": "/e2e-wchmr1"},
		"forward": map[string]any{
			"destinations":        []map[string]any{{"destinationId": destA, "weight": 50}, {"destinationId": destB, "weight": 50}},
			"destinationBalancing": bal,
		},
	})
	defer apiDelete(t, "/routes/"+id(r1))

	_, r2 := apiPost(t, "/routes", map[string]any{
		"name": "e2e-wchmr2", "match": map[string]any{"pathPrefix": "/e2e-wchmr2"},
		"forward": map[string]any{
			"destinations":        []map[string]any{{"destinationId": destA, "weight": 50}, {"destinationId": destB, "weight": 50}},
			"destinationBalancing": bal,
		},
	})
	defer apiDelete(t, "/routes/"+id(r2))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	broken := 0
	for i := 0; i < 200; i++ {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Timeout: 5e9, Jar: jar}
		d1 := clientGet(t, c, "/e2e-wchmr1")
		d2 := clientGet(t, c, "/e2e-wchmr2")
		for r := 0; r < 25; r++ {
			if clientGet(t, c, "/e2e-wchmr1") != d1 || clientGet(t, c, "/e2e-wchmr2") != d2 {
				broken++
				break
			}
		}
	}
	if broken > 0 {
		t.Errorf("isolation broken: %d/200 users", broken)
	}
	t.Logf("isolation: %d/200 users stable across 2 routes (total %d reqs)", 200-broken, 200*50)
}

// TestE2E_Proxy_WCH_Concurrent verifies stickiness under concurrent load.
func TestE2E_Proxy_WCH_Concurrent(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-wchc-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-wchc-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-wchc",
		"match": map[string]any{"pathPrefix": "/e2e-wchc"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 50},
				{"destinationId": destB, "weight": 50},
			},
			"destinationBalancing": destBalancing("_vrata_wchc", "1h"),
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	const users = 50
	const reqsPerUser = 100
	var wg sync.WaitGroup
	var broken atomic.Int32
	for u := 0; u < users; u++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			jar, _ := cookiejar.New(nil)
			c := &http.Client{Timeout: 5e9, Jar: jar}
			first := clientGet(t, c, fmt.Sprintf("/e2e-wchc?u=%d", idx))
			for r := 1; r < reqsPerUser; r++ {
				if clientGet(t, c, fmt.Sprintf("/e2e-wchc?u=%d&r=%d", idx, r)) != first {
					broken.Add(1)
					return
				}
			}
		}(u)
	}
	wg.Wait()
	b := int(broken.Load())
	if b > 0 {
		t.Errorf("concurrent stickiness broken: %d/%d users", b, users)
	}
	t.Logf("concurrent: %d/%d users stable (%d reqs each, total %d)", users-b, users, reqsPerUser, users*reqsPerUser)
}

// ─── STICKY (falls back to WCH when no Redis) ─────────────────────────────

// TestE2E_Proxy_Sticky_FallbackToWCH verifies that STICKY without Redis
// falls back to WEIGHTED_CONSISTENT_HASH behaviour (stickiness with some
// disruption on weight change).
func TestE2E_Proxy_Sticky_FallbackToWCH(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-stk-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-stk-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	stickyBal := map[string]any{
		"algorithm": "STICKY",
		"sticky": map[string]any{
			"cookie": map[string]any{"name": "_vrata_stk", "ttl": "1h"},
		},
	}

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-stk",
		"match": map[string]any{"pathPrefix": "/e2e-stk"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 60},
				{"destinationId": destB, "weight": 40},
			},
			"destinationBalancing": stickyBal,
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	const users = 100
	broken := 0
	for u := 0; u < users; u++ {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Timeout: 5e9, Jar: jar}
		first := clientGet(t, c, fmt.Sprintf("/e2e-stk?u=%d", u))
		for r := 1; r < 50; r++ {
			if clientGet(t, c, fmt.Sprintf("/e2e-stk?u=%d&r=%d", u, r)) != first {
				broken++
				break
			}
		}
	}
	if broken > 0 {
		t.Errorf("sticky fallback broken: %d/%d users", broken, users)
	}
	t.Logf("sticky fallback: %d/%d users stable (total %d reqs)", users-broken, users, users*50)
}

// TestE2E_Proxy_Sticky_ZeroDisruption verifies that STICKY with Redis
// achieves zero disruption when weights change. Existing clients stay on
// their original destination; new clients distribute according to new weights.
// Requires Redis on localhost:6379 (started by e2e infrastructure).
func TestE2E_Proxy_Sticky_ZeroDisruption(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-stkz-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-stkz-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	stickyBal := map[string]any{
		"algorithm": "STICKY",
		"sticky": map[string]any{
			"cookie": map[string]any{"name": "_vrata_stkz", "ttl": "1h"},
		},
	}

	routeBody := func(wA, wB int) map[string]any {
		return map[string]any{
			"name":  "e2e-stkz",
			"match": map[string]any{"pathPrefix": "/e2e-stkz"},
			"forward": map[string]any{
				"destinations": []map[string]any{
					{"destinationId": destA, "weight": wA},
					{"destinationId": destB, "weight": wB},
				},
				"destinationBalancing": stickyBal,
			},
		}
	}

	_, route := apiPost(t, "/routes", routeBody(80, 20))
	routeID := id(route)
	defer apiDelete(t, "/routes/"+routeID)
	snap1 := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap1)

	// Pin 200 users at 80/20.
	type pinned struct {
		jar  http.CookieJar
		dest string
	}
	users := make([]pinned, 200)
	for i := range users {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Timeout: 5e9, Jar: jar}
		users[i] = pinned{jar: jar, dest: clientGet(t, c, fmt.Sprintf("/e2e-stkz?u=%d", i))}
	}

	// Verify initial distribution.
	initA, initB := 0, 0
	for _, u := range users {
		switch u.dest {
		case "A":
			initA++
		case "B":
			initB++
		}
	}
	t.Logf("initial: A=%d B=%d (target 80/20)", initA, initB)

	// Change weights to 20/80.
	apiPut(t, "/routes/"+routeID, routeBody(20, 80))
	snap2 := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap2)

	// Existing users must ALL stay on their original destination (zero disruption).
	moved := 0
	for i, u := range users {
		c := &http.Client{Timeout: 5e9, Jar: u.jar}
		for r := 0; r < 25; r++ {
			got := clientGet(t, c, fmt.Sprintf("/e2e-stkz?u=%d&r=%d", i, r))
			if got != u.dest {
				moved++
				break
			}
		}
	}
	if moved > 0 {
		t.Errorf("STICKY zero disruption FAILED: %d/%d users moved after weight change 80/20→20/80", moved, len(users))
	}
	t.Logf("zero disruption: %d/%d users stayed (25 reqs each, total %d)", len(users)-moved, len(users), len(users)*25)

	// New users should respect new weights (20/80).
	newA, newB := 0, 0
	for i := 0; i < 500; i++ {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Timeout: 5e9, Jar: jar}
		switch clientGet(t, c, fmt.Sprintf("/e2e-stkz?new=%d", i)) {
		case "A":
			newA++
		case "B":
			newB++
		}
	}
	assertBW(t, "new A", newA, 500, 0.20, 0.07)
	assertBW(t, "new B", newB, 500, 0.80, 0.07)
	t.Logf("new users: A=%d (%.1f%%) B=%d (%.1f%%)", newA, pct(newA, 500), newB, pct(newB, 500))
}

// TestE2E_Proxy_Sticky_DestinationRemoved verifies that when a pinned
// destination is removed, the client gets reassigned via weighted random.
func TestE2E_Proxy_Sticky_DestinationRemoved(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-stkr-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-stkr-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	stickyBal := map[string]any{
		"algorithm": "STICKY",
		"sticky": map[string]any{
			"cookie": map[string]any{"name": "_vrata_stkr", "ttl": "1h"},
		},
	}

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-stkr",
		"match": map[string]any{"pathPrefix": "/e2e-stkr"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 50},
				{"destinationId": destB, "weight": 50},
			},
			"destinationBalancing": stickyBal,
		},
	})
	routeID := id(route)
	defer apiDelete(t, "/routes/"+routeID)
	snap1 := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap1)

	// Pin a user.
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Timeout: 5e9, Jar: jar}
	first := clientGet(t, c, "/e2e-stkr")

	// Remove the pinned destination from the route.
	remaining := destB
	expected := "B"
	if first == "B" {
		remaining = destA
		expected = "A"
	}
	apiPut(t, "/routes/"+routeID, map[string]any{
		"name":  "e2e-stkr",
		"match": map[string]any{"pathPrefix": "/e2e-stkr"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": remaining, "weight": 100},
			},
			"destinationBalancing": stickyBal,
		},
	})
	snap2 := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap2)

	got := clientGet(t, c, "/e2e-stkr")
	if got != expected {
		t.Errorf("after removal: expected %s, got %s", expected, got)
	}
	t.Logf("destination removed: %s → %s (reassigned correctly)", first, got)
}

// TestE2E_Proxy_Sticky_Concurrent verifies STICKY under concurrent load.
func TestE2E_Proxy_Sticky_Concurrent(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-stkcc-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-stkcc-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	stickyBal := map[string]any{
		"algorithm": "STICKY",
		"sticky": map[string]any{
			"cookie": map[string]any{"name": "_vrata_stkcc", "ttl": "1h"},
		},
	}

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-stkcc",
		"match": map[string]any{"pathPrefix": "/e2e-stkcc"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 50},
				{"destinationId": destB, "weight": 50},
			},
			"destinationBalancing": stickyBal,
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	const numUsers = 50
	const reqsPerUser = 100
	var wg sync.WaitGroup
	var broken atomic.Int32
	for u := 0; u < numUsers; u++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			jar, _ := cookiejar.New(nil)
			c := &http.Client{Timeout: 5e9, Jar: jar}
			first := clientGet(t, c, fmt.Sprintf("/e2e-stkcc?u=%d", idx))
			for r := 1; r < reqsPerUser; r++ {
				if clientGet(t, c, fmt.Sprintf("/e2e-stkcc?u=%d&r=%d", idx, r)) != first {
					broken.Add(1)
					return
				}
			}
		}(u)
	}
	wg.Wait()
	b := int(broken.Load())
	if b > 0 {
		t.Errorf("concurrent STICKY broken: %d/%d users", b, numUsers)
	}
	t.Logf("concurrent STICKY: %d/%d stable (%d reqs each, total %d)", numUsers-b, numUsers, reqsPerUser, numUsers*reqsPerUser)
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func clientGet(t *testing.T, c *http.Client, path string) string {
	t.Helper()
	resp, err := c.Get(proxyURL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf)
	resp.Body.Close()
	return string(buf[:n])
}

func sendN(t *testing.T, path string, n int) (countA, countB int) {
	t.Helper()
	for i := 0; i < n; i++ {
		_, _, body := proxyGet(t, path, nil)
		switch body {
		case "A":
			countA++
		case "B":
			countB++
		}
	}
	return
}

func assertBW(t *testing.T, label string, got, total int, expected, tolerance float64) {
	t.Helper()
	actual := float64(got) / float64(total)
	if math.Abs(actual-expected) > tolerance {
		t.Errorf("%s: expected ~%.0f%% got %.1f%% (%d/%d)",
			label, expected*100, actual*100, got, total)
	}
}

func pct(n, total int) float64 { return float64(n) * 100 / float64(total) }
