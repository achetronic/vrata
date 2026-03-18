package e2e

import (
	"math"
	"net/http"
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
			"backends": []map[string]any{
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
			"backends": []map[string]any{
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
			"backends": []map[string]any{
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
			"backends": []map[string]any{
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
			"backends": []map[string]any{
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
