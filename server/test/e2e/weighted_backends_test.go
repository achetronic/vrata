package e2e

import (
	"fmt"
	"math"
	"net/http"
	"net/http/cookiejar"
	"sync"
	"testing"
)

func TestE2E_Proxy_WeightedBackends(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("A"))
	})
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("B"))
	})

	destA := createDestination(t, "e2e-wa", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-wb", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-weights",
		"match": map[string]any{"pathPrefix": "/e2e-weights"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 80},
				{"destinationId": destB, "weight": 20},
			},
		},
	})
	routeID := id(route)
	defer apiDelete(t, "/routes/"+routeID)

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	// Round 1: 80/20 split.
	countA, countB := sendAndCount(t, "/e2e-weights", 5000)
	assertWeight(t, "round1 A", countA, 5000, 0.80, 0.05)
	assertWeight(t, "round1 B", countB, 5000, 0.20, 0.05)

	// Round 2: flip to 30/70 via API update, new snapshot.
	apiPut(t, "/routes/"+routeID, map[string]any{
		"name":  "e2e-weights",
		"match": map[string]any{"pathPrefix": "/e2e-weights"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 30},
				{"destinationId": destB, "weight": 70},
			},
		},
	})
	snapID2 := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID2)

	upA.requestCount.Store(0)
	upB.requestCount.Store(0)

	countA, countB = sendAndCount(t, "/e2e-weights", 5000)
	assertWeight(t, "round2 A", countA, 5000, 0.30, 0.05)
	assertWeight(t, "round2 B", countB, 5000, 0.70, 0.05)

	// Round 3: equal weights 50/50.
	apiPut(t, "/routes/"+routeID, map[string]any{
		"name":  "e2e-weights",
		"match": map[string]any{"pathPrefix": "/e2e-weights"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 50},
				{"destinationId": destB, "weight": 50},
			},
		},
	})
	snapID3 := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID3)

	upA.requestCount.Store(0)
	upB.requestCount.Store(0)

	countA, countB = sendAndCount(t, "/e2e-weights", 5000)
	assertWeight(t, "round3 A", countA, 5000, 0.50, 0.05)
	assertWeight(t, "round3 B", countB, 5000, 0.50, 0.05)

	// Round 4: 100/0 — all traffic to A.
	apiPut(t, "/routes/"+routeID, map[string]any{
		"name":  "e2e-weights",
		"match": map[string]any{"pathPrefix": "/e2e-weights"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 100},
				{"destinationId": destB, "weight": 0},
			},
		},
	})
	snapID4 := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID4)

	upA.requestCount.Store(0)
	upB.requestCount.Store(0)

	countA, countB = sendAndCount(t, "/e2e-weights", 5000)
	if countA != 5000 {
		t.Errorf("round4: expected 100%% to A, got A=%d B=%d", countA, countB)
	}
}

func TestE2E_Proxy_ThreeBackendsWeighted(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })
	upC := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("C")) })

	destA := createDestination(t, "e2e-3wa", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-3wb", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)
	destC := createDestination(t, "e2e-3wc", upC.host(), upC.port())
	defer apiDelete(t, "/destinations/"+destC)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-3weights",
		"match": map[string]any{"pathPrefix": "/e2e-3weights"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 60},
				{"destinationId": destB, "weight": 30},
				{"destinationId": destC, "weight": 10},
			},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	counts := map[string]int{"A": 0, "B": 0, "C": 0}
	total := 5000
	for i := 0; i < total; i++ {
		_, _, body := proxyGet(t, "/e2e-3weights", nil)
		counts[body]++
	}

	assertWeight(t, "A", counts["A"], total, 0.60, 0.05)
	assertWeight(t, "B", counts["B"], total, 0.30, 0.05)
	assertWeight(t, "C", counts["C"], total, 0.10, 0.05)
}

func sendAndCount(t *testing.T, path string, n int) (countA, countB int) {
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

func assertWeight(t *testing.T, label string, got, total int, expectedRatio, tolerance float64) {
	t.Helper()
	actual := float64(got) / float64(total)
	if math.Abs(actual-expectedRatio) > tolerance {
		t.Errorf("%s: expected ~%.0f%% got %.1f%% (%d/%d)",
			label, expectedRatio*100, actual*100, got, total)
	}
}

// TestE2E_Proxy_WeightedConsistentHashStickiness verifies that the weighted
// consistent hash ring (destinationBalancing) keeps each client pinned to the
// same destination across weight changes and a high volume of requests.
//
// The test:
//  1. Pins 200 unique users (cookie jars) to A or B at 70/30.
//  2. Changes weights to 30/70 and activates a new snapshot.
//  3. Sends 50 more requests per user and asserts each user stays on the
//     same destination they were originally pinned to (consistent hash
//     property: weight changes should not reassign existing clients).
//  4. Verifies overall distribution across a fresh set of 500 users
//     reflects the new 30/70 weights within tolerance.
func TestE2E_Proxy_WeightedConsistentHashStickiness(t *testing.T) {
	upA := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("A")) })
	upB := startUpstream(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("B")) })

	destA := createDestination(t, "e2e-chs-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-chs-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "e2e-chs",
		"match": map[string]any{"pathPrefix": "/e2e-chs"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 70},
				{"destinationId": destB, "weight": 30},
			},
			"destinationBalancing": destBalancing("_vrata_chs", "1h"),
		},
	})
	routeID := id(route)
	defer apiDelete(t, "/routes/"+routeID)

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	// Phase 1: pin 200 users at 70/30.
	const users = 200
	type userState struct {
		jar  http.CookieJar
		dest string
	}
	pinned := make([]userState, users)
	for i := range pinned {
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Timeout: 5e9, Jar: jar}
		resp, err := client.Get(proxyURL + fmt.Sprintf("/e2e-chs?u=%d", i))
		if err != nil {
			t.Fatalf("user %d initial request: %v", i, err)
		}
		buf := make([]byte, 4)
		n, _ := resp.Body.Read(buf)
		resp.Body.Close()
		pinned[i] = userState{jar: jar, dest: string(buf[:n])}
	}

	// Phase 2: flip weights to 30/70.
	apiPut(t, "/routes/"+routeID, map[string]any{
		"name":  "e2e-chs",
		"match": map[string]any{"pathPrefix": "/e2e-chs"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 30},
				{"destinationId": destB, "weight": 70},
			},
			"destinationBalancing": destBalancing("_vrata_chs", "1h"),
		},
	})
	snapID2 := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID2)

	// Phase 3: each pinned user sends 50 more requests — must stay on same dest.
	broken := 0
	const reqs = 50
	for i, u := range pinned {
		client := &http.Client{Timeout: 5e9, Jar: u.jar}
		for r := 0; r < reqs; r++ {
			resp, err := client.Get(proxyURL + fmt.Sprintf("/e2e-chs?u=%d&r=%d", i, r))
			if err != nil {
				t.Fatalf("user %d req %d: %v", i, r, err)
			}
			buf := make([]byte, 4)
			n, _ := resp.Body.Read(buf)
			resp.Body.Close()
			if got := string(buf[:n]); got != u.dest {
				broken++
				break
			}
		}
	}
	if broken > 0 {
		t.Errorf("consistent hash broken: %d/%d users switched destination after weight change", broken, users)
	}

	// Phase 4: fresh users reflect new 30/70 distribution.
	const freshUsers = 500
	countA, countB := 0, 0
	for i := 0; i < freshUsers; i++ {
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Timeout: 5e9, Jar: jar}
		resp, err := client.Get(proxyURL + fmt.Sprintf("/e2e-chs?fresh=%d", i))
		if err != nil {
			t.Fatalf("fresh user %d: %v", i, err)
		}
		buf := make([]byte, 4)
		n, _ := resp.Body.Read(buf)
		resp.Body.Close()
		switch string(buf[:n]) {
		case "A":
			countA++
		case "B":
			countB++
		}
	}
	assertWeight(t, "fresh A after reweight", countA, freshUsers, 0.30, 0.07)
	assertWeight(t, "fresh B after reweight", countB, freshUsers, 0.70, 0.07)
}

// TestE2E_Proxy_CombinedConsistentHashAndStickiness exercises both levels
// simultaneously:
//
//   - destination-level consistency: each user is pinned to A or B via the
//     weighted consistent hash ring (destinationBalancing).
//   - within-destination stickiness: each upstream records which session IDs
//     it sees; a given session ID must never appear on both upstreams.
//
// The test runs 3 weight epochs (80/20 → 50/50 → 20/80) while 50 long-lived
// users fire 100 requests each concurrently. Throughout all epochs:
//
//  1. No user ever switches destination (consistent hash property).
//  2. Every request from a given user lands on exactly one upstream
//     (within-destination stickiness via cookie).
//  3. A fresh cohort of 300 users after each reweight reflects the new
//     distribution within a 8% tolerance.
func TestE2E_Proxy_CombinedConsistentHashAndStickiness(t *testing.T) {
	type seenUpstream struct {
		mu   sync.Mutex
		seen map[string]string // sessionID → "A" or "B"
	}
	tracker := &seenUpstream{seen: make(map[string]string)}

	makeHandler := func(label string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			sid := ""
			for _, c := range r.Cookies() {
				if c.Name == "_vrata_combo" {
					sid = c.Value
					break
				}
			}
			if sid != "" {
				tracker.mu.Lock()
				if prev, ok := tracker.seen[sid]; ok && prev != label {
					w.Header().Set("X-Conflict", prev+"-then-"+label)
				}
				tracker.seen[sid] = label
				tracker.mu.Unlock()
			}
			w.Write([]byte(label))
		}
	}

	upA := startUpstream(t, makeHandler("A"))
	upB := startUpstream(t, makeHandler("B"))

	destA := createDestination(t, "e2e-combo-a", upA.host(), upA.port())
	defer apiDelete(t, "/destinations/"+destA)
	destB := createDestination(t, "e2e-combo-b", upB.host(), upB.port())
	defer apiDelete(t, "/destinations/"+destB)

	pinCfg := destBalancing("_vrata_combo", "1h")

	routeBody := func(wA, wB int) map[string]any {
		return map[string]any{
			"name":  "e2e-combo",
			"match": map[string]any{"pathPrefix": "/e2e-combo"},
			"forward": map[string]any{
				"destinations": []map[string]any{
					{"destinationId": destA, "weight": wA},
					{"destinationId": destB, "weight": wB},
				},
				"destinationBalancing": pinCfg,
			},
		}
	}

	_, route := apiPost(t, "/routes", routeBody(80, 20))
	routeID := id(route)
	defer apiDelete(t, "/routes/"+routeID)

	snapID := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snapID)

	// Spin up 50 long-lived users, each with their own jar.
	const users = 50
	type user struct {
		jar    http.CookieJar
		client *http.Client
		dest   string // first destination seen
	}
	cohort := make([]user, users)
	for i := range cohort {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Timeout: 5e9, Jar: jar}
		resp, err := c.Get(proxyURL + fmt.Sprintf("/e2e-combo?u=%d&epoch=0", i))
		if err != nil {
			t.Fatalf("user %d warmup: %v", i, err)
		}
		buf := make([]byte, 4)
		n, _ := resp.Body.Read(buf)
		resp.Body.Close()
		if resp.Header.Get("X-Conflict") != "" {
			t.Fatalf("user %d: conflict on warmup: %s", i, resp.Header.Get("X-Conflict"))
		}
		cohort[i] = user{jar: jar, client: c, dest: string(buf[:n])}
	}

	// Run epochs, changing weights each time.
	epochs := []struct{ wA, wB int }{{50, 50}, {20, 80}}
	for epoch, e := range epochs {
		apiPut(t, "/routes/"+routeID, routeBody(e.wA, e.wB))
		snap := activateSnapshot(t)
		defer apiDelete(t, "/snapshots/"+snap)

		// All 50 users fire 100 requests concurrently.
		var wg sync.WaitGroup
		conflicts := make([]string, users)
		for i, u := range cohort {
			wg.Add(1)
			go func(idx int, u user) {
				defer wg.Done()
				c := u.client
				for r := 0; r < 100; r++ {
					resp, err := c.Get(proxyURL + fmt.Sprintf("/e2e-combo?u=%d&epoch=%d&r=%d", idx, epoch+1, r))
					if err != nil {
						conflicts[idx] = fmt.Sprintf("request error: %v", err)
						return
					}
					buf := make([]byte, 4)
					n, _ := resp.Body.Read(buf)
					resp.Body.Close()
					got := string(buf[:n])
					if conflict := resp.Header.Get("X-Conflict"); conflict != "" {
						conflicts[idx] = fmt.Sprintf("epoch %d req %d: upstream saw two dests: %s", epoch+1, r, conflict)
						return
					}
					if got != u.dest {
						conflicts[idx] = fmt.Sprintf("epoch %d req %d: pinned to %s, got %s", epoch+1, r, u.dest, got)
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

		// Fresh cohort reflects new distribution.
		countA, countB := 0, 0
		for i := 0; i < 300; i++ {
			jar, _ := cookiejar.New(nil)
			c := &http.Client{Timeout: 5e9, Jar: jar}
			resp, err := c.Get(proxyURL + fmt.Sprintf("/e2e-combo?fresh=%d&epoch=%d", i, epoch+1))
			if err != nil {
				t.Fatalf("fresh user %d epoch %d: %v", i, epoch+1, err)
			}
			buf := make([]byte, 4)
			n, _ := resp.Body.Read(buf)
			resp.Body.Close()
			switch string(buf[:n]) {
			case "A":
				countA++
			case "B":
				countB++
			}
		}
		ratioA := float64(e.wA) / float64(e.wA+e.wB)
		ratioB := float64(e.wB) / float64(e.wA+e.wB)
		assertWeight(t, fmt.Sprintf("epoch%d fresh A", epoch+1), countA, 300, ratioA, 0.08)
		assertWeight(t, fmt.Sprintf("epoch%d fresh B", epoch+1), countB, 300, ratioB, 0.08)
	}
}
