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

// ─── Helpers for multi-endpoint tests ──────────────────────────────────────

// labeledUpstream is a test upstream that responds with its label.
type labeledUpstream struct {
	*testUpstream
	label string
}

func startLabeledUpstreams(t *testing.T, n int) []labeledUpstream {
	t.Helper()
	ups := make([]labeledUpstream, n)
	for i := range ups {
		label := fmt.Sprintf("ep%d", i)
		up := startUpstream(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(label))
		})
		ups[i] = labeledUpstream{testUpstream: up, label: label}
	}
	return ups
}

func endpointList(ups []labeledUpstream) []map[string]any {
	eps := make([]map[string]any, len(ups))
	for i, u := range ups {
		eps[i] = map[string]any{"host": u.host(), "port": u.port()}
	}
	return eps
}

func createMultiEndpointDest(t *testing.T, name string, ups []labeledUpstream, eb map[string]any) string {
	t.Helper()
	dest := map[string]any{
		"name":      name,
		"host":      ups[0].host(),
		"port":      ups[0].port(),
		"endpoints": endpointList(ups),
	}
	if eb != nil {
		dest["options"] = map[string]any{"endpointBalancing": eb}
	}
	_, d := apiPost(t, "/destinations", dest)
	if d["id"] == nil {
		t.Fatalf("create destination %s failed: %v", name, d)
	}
	return id(d)
}

func assertEpBW(t *testing.T, label string, got, total int, expected, tolerance float64) {
	t.Helper()
	actual := float64(got) / float64(total)
	if math.Abs(actual-expected) > tolerance {
		t.Errorf("%s: expected ~%.0f%% got %.1f%% (%d/%d)",
			label, expected*100, actual*100, got, total)
	}
}

// ─── ROUND_ROBIN ───────────────────────────────────────────────────────────

// TestE2E_Endpoint_RoundRobin verifies round-robin distributes evenly
// across 3 endpoints within a single destination over 6000 requests.
func TestE2E_Endpoint_RoundRobin(t *testing.T) {
	ups := startLabeledUpstreams(t, 3)
	destID := createMultiEndpointDest(t, "ep-rr", ups, map[string]any{
		"algorithm": "ROUND_ROBIN",
	})
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "ep-rr",
		"match": map[string]any{"pathPrefix": "/ep-rr"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	counts := map[string]int{}
	const total = 6000
	for i := 0; i < total; i++ {
		_, _, body := proxyGet(t, "/ep-rr", nil)
		counts[body]++
	}
	for i, u := range ups {
		assertEpBW(t, u.label, counts[u.label], total, 1.0/3.0, 0.05)
		t.Logf("ep%d: %d (%.1f%%)", i, counts[u.label], float64(counts[u.label])*100/float64(total))
	}
}

// ─── RANDOM ────────────────────────────────────────────────────────────────

// TestE2E_Endpoint_Random verifies random distributes roughly evenly
// across 3 endpoints over 6000 requests.
func TestE2E_Endpoint_Random(t *testing.T) {
	ups := startLabeledUpstreams(t, 3)
	destID := createMultiEndpointDest(t, "ep-rand", ups, map[string]any{
		"algorithm": "RANDOM",
	})
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "ep-rand",
		"match": map[string]any{"pathPrefix": "/ep-rand"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	counts := map[string]int{}
	const total = 6000
	for i := 0; i < total; i++ {
		_, _, body := proxyGet(t, "/ep-rand", nil)
		counts[body]++
	}
	for i, u := range ups {
		assertEpBW(t, u.label, counts[u.label], total, 1.0/3.0, 0.08)
		t.Logf("ep%d: %d (%.1f%%)", i, counts[u.label], float64(counts[u.label])*100/float64(total))
	}
}

// ─── RING_HASH with header ─────────────────────────────────────────────────

// TestE2E_Endpoint_RingHash_Header verifies that the same X-User-ID header
// consistently routes to the same endpoint across 5000 requests.
func TestE2E_Endpoint_RingHash_Header(t *testing.T) {
	ups := startLabeledUpstreams(t, 3)
	destID := createMultiEndpointDest(t, "ep-rh", ups, map[string]any{
		"algorithm": "RING_HASH",
		"ringHash": map[string]any{
			"hashPolicy": []map[string]any{
				{"header": map[string]any{"name": "X-User-ID"}},
			},
		},
	})
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "ep-rh",
		"match": map[string]any{"pathPrefix": "/ep-rh"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	const users = 100
	const reqsPerUser = 50
	broken := 0
	for u := 0; u < users; u++ {
		hdr := map[string]string{"X-User-ID": fmt.Sprintf("user-%d", u)}
		_, _, first := proxyGet(t, "/ep-rh", hdr)
		for r := 1; r < reqsPerUser; r++ {
			_, _, body := proxyGet(t, "/ep-rh", hdr)
			if body != first {
				broken++
				break
			}
		}
	}
	if broken > 0 {
		t.Errorf("ring hash broken: %d/%d users got different endpoints", broken, users)
	}
	t.Logf("ring hash: %d/%d users stable (%d reqs each, total %d)", users-broken, users, reqsPerUser, users*reqsPerUser)
}

// ─── RING_HASH with cookie ─────────────────────────────────────────────────

// TestE2E_Endpoint_RingHash_Cookie verifies that the auto-generated cookie
// pins each client to the same endpoint across 5000 requests.
func TestE2E_Endpoint_RingHash_Cookie(t *testing.T) {
	ups := startLabeledUpstreams(t, 3)
	destID := createMultiEndpointDest(t, "ep-rhc", ups, map[string]any{
		"algorithm": "RING_HASH",
		"ringHash": map[string]any{
			"hashPolicy": []map[string]any{
				{"cookie": map[string]any{"name": "_vrata_ep_test", "ttl": "1h"}},
			},
		},
	})
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "ep-rhc",
		"match": map[string]any{"pathPrefix": "/ep-rhc"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	const users = 100
	const reqsPerUser = 50
	broken := 0
	for u := 0; u < users; u++ {
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Timeout: 5e9, Jar: jar}
		first := clientGet(t, c, fmt.Sprintf("/ep-rhc?u=%d", u))
		for r := 1; r < reqsPerUser; r++ {
			if clientGet(t, c, fmt.Sprintf("/ep-rhc?u=%d&r=%d", u, r)) != first {
				broken++
				break
			}
		}
	}
	if broken > 0 {
		t.Errorf("ring hash cookie broken: %d/%d users got different endpoints", broken, users)
	}
	t.Logf("ring hash cookie: %d/%d users stable (%d reqs each, total %d)", users-broken, users, reqsPerUser, users*reqsPerUser)
}

// ─── RING_HASH with sourceIP ───────────────────────────────────────────────

// TestE2E_Endpoint_RingHash_SourceIP verifies that sourceIP hashing pins
// the same client IP to the same endpoint across 5000 requests.
func TestE2E_Endpoint_RingHash_SourceIP(t *testing.T) {
	ups := startLabeledUpstreams(t, 3)
	destID := createMultiEndpointDest(t, "ep-rhs", ups, map[string]any{
		"algorithm": "RING_HASH",
		"ringHash": map[string]any{
			"hashPolicy": []map[string]any{
				{"sourceIP": map[string]any{"enabled": true}},
			},
		},
	})
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "ep-rhs",
		"match": map[string]any{"pathPrefix": "/ep-rhs"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	const total = 5000
	_, _, first := proxyGet(t, "/ep-rhs", nil)
	broken := 0
	for i := 1; i < total; i++ {
		_, _, body := proxyGet(t, "/ep-rhs", nil)
		if body != first {
			broken++
		}
	}
	if broken > 0 {
		t.Errorf("sourceIP hash: %d/%d requests went to different endpoint", broken, total)
	}
	t.Logf("sourceIP hash: all %d requests went to %s", total, first)
}

// ─── MAGLEV ────────────────────────────────────────────────────────────────

// TestE2E_Endpoint_Maglev verifies maglev consistent hash with header policy
// pins users to the same endpoint across 5000 requests.
func TestE2E_Endpoint_Maglev(t *testing.T) {
	ups := startLabeledUpstreams(t, 3)
	destID := createMultiEndpointDest(t, "ep-mag", ups, map[string]any{
		"algorithm": "MAGLEV",
		"maglev": map[string]any{
			"tableSize": 65537,
			"hashPolicy": []map[string]any{
				{"header": map[string]any{"name": "X-User-ID"}},
			},
		},
	})
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "ep-mag",
		"match": map[string]any{"pathPrefix": "/ep-mag"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	const users = 100
	const reqsPerUser = 50
	broken := 0
	for u := 0; u < users; u++ {
		hdr := map[string]string{"X-User-ID": fmt.Sprintf("user-%d", u)}
		_, _, first := proxyGet(t, "/ep-mag", hdr)
		for r := 1; r < reqsPerUser; r++ {
			_, _, body := proxyGet(t, "/ep-mag", hdr)
			if body != first {
				broken++
				break
			}
		}
	}
	if broken > 0 {
		t.Errorf("maglev broken: %d/%d users got different endpoints", broken, users)
	}
	t.Logf("maglev: %d/%d users stable (%d reqs each, total %d)", users-broken, users, reqsPerUser, users*reqsPerUser)
}

// ─── Combined L1 + L2 ─────────────────────────────────────────────────────

// TestE2E_Endpoint_CombinedL1WCH_L2RingHash tests both levels together:
// L1: WEIGHTED_CONSISTENT_HASH picks destination A or B (2 destinations)
// L2: RING_HASH picks endpoint within the chosen destination (3 endpoints each)
// Verifies: destination pinning AND endpoint pinning simultaneously.
func TestE2E_Endpoint_CombinedL1WCH_L2RingHash(t *testing.T) {
	upsA := startLabeledUpstreams(t, 3)
	upsB := startLabeledUpstreams(t, 3)

	ebConfig := map[string]any{
		"algorithm": "RING_HASH",
		"ringHash": map[string]any{
			"hashPolicy": []map[string]any{
				{"header": map[string]any{"name": "X-Session"}},
			},
		},
	}

	destA := createMultiEndpointDest(t, "ep-combo-a", upsA, ebConfig)
	defer apiDelete(t, "/destinations/"+destA)
	destB := createMultiEndpointDest(t, "ep-combo-b", upsB, ebConfig)
	defer apiDelete(t, "/destinations/"+destB)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "ep-combo",
		"match": map[string]any{"pathPrefix": "/ep-combo"},
		"forward": map[string]any{
			"destinations": []map[string]any{
				{"destinationId": destA, "weight": 50},
				{"destinationId": destB, "weight": 50},
			},
			"destinationBalancing": destBalancing("_vrata_epc", "1h"),
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

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
			req, _ := http.NewRequest("GET", proxyURL+fmt.Sprintf("/ep-combo?u=%d", idx), nil)
			req.Header.Set("X-Session", fmt.Sprintf("sess-%d", idx))
			resp, err := c.Do(req)
			if err != nil {
				broken.Add(1)
				return
			}
			buf := make([]byte, 16)
			n, _ := resp.Body.Read(buf)
			resp.Body.Close()
			first := string(buf[:n])

			for r := 1; r < reqsPerUser; r++ {
				req, _ := http.NewRequest("GET", proxyURL+fmt.Sprintf("/ep-combo?u=%d&r=%d", idx, r), nil)
				req.Header.Set("X-Session", fmt.Sprintf("sess-%d", idx))
				resp, err := c.Do(req)
				if err != nil {
					broken.Add(1)
					return
				}
				buf := make([]byte, 16)
				n, _ := resp.Body.Read(buf)
				resp.Body.Close()
				if string(buf[:n]) != first {
					broken.Add(1)
					return
				}
			}
		}(u)
	}
	wg.Wait()
	b := int(broken.Load())
	if b > 0 {
		t.Errorf("combined L1+L2 broken: %d/%d users got different responses", b, users)
	}
	t.Logf("combined L1(WCH)+L2(RING_HASH): %d/%d users stable (%d reqs each, total %d)", users-b, users, reqsPerUser, users*reqsPerUser)
}

// ─── Default (no endpointBalancing) ────────────────────────────────────────

// TestE2E_Endpoint_DefaultDistribution verifies that without explicit
// endpointBalancing, multiple endpoints still receive traffic.
func TestE2E_Endpoint_DefaultDistribution(t *testing.T) {
	ups := startLabeledUpstreams(t, 3)
	destID := createMultiEndpointDest(t, "ep-def", ups, nil)
	defer apiDelete(t, "/destinations/"+destID)

	_, route := apiPost(t, "/routes", map[string]any{
		"name":  "ep-def",
		"match": map[string]any{"pathPrefix": "/ep-def"},
		"forward": map[string]any{
			"destinations": []map[string]any{{"destinationId": destID, "weight": 100}},
		},
	})
	defer apiDelete(t, "/routes/"+id(route))
	snap := activateSnapshot(t)
	defer apiDelete(t, "/snapshots/"+snap)

	counts := map[string]int{}
	const total = 6000
	for i := 0; i < total; i++ {
		_, _, body := proxyGet(t, "/ep-def", nil)
		counts[body]++
	}
	seen := 0
	for _, u := range ups {
		if counts[u.label] > 0 {
			seen++
		}
		t.Logf("%s: %d (%.1f%%)", u.label, counts[u.label], float64(counts[u.label])*100/float64(total))
	}
	if seen < 2 {
		t.Errorf("expected traffic to at least 2 endpoints, only saw %d", seen)
	}
}
