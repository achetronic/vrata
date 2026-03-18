package proxy

import (
	"hash/crc32"
	"testing"

	"github.com/achetronic/vrata/internal/model"
)

func TestDestinationRingDeterministic(t *testing.T) {
	backends := []model.DestinationRef{
		{DestinationID: "a", Weight: 50},
		{DestinationID: "b", Weight: 50},
	}
	ring := buildDestinationRing(backends)

	key := crc32.ChecksumIEEE([]byte("session-1:route-1"))
	first := ring.Pick(key)
	for i := 0; i < 100; i++ {
		if got := ring.Pick(key); got != first {
			t.Fatalf("non-deterministic: first=%s, got=%s on iteration %d", first, got, i)
		}
	}
}

func TestDestinationRingWeightDistribution(t *testing.T) {
	backends := []model.DestinationRef{
		{DestinationID: "heavy", Weight: 90},
		{DestinationID: "light", Weight: 10},
	}
	ring := buildDestinationRing(backends)

	counts := map[string]int{}
	total := 10000
	for i := 0; i < total; i++ {
		key := crc32.ChecksumIEEE([]byte("sid-" + itoa(i) + ":route"))
		counts[ring.Pick(key)]++
	}

	heavyPct := float64(counts["heavy"]) / float64(total)
	if heavyPct < 0.80 || heavyPct > 0.97 {
		t.Errorf("expected ~90%% heavy, got %.1f%% (heavy=%d, light=%d)", heavyPct*100, counts["heavy"], counts["light"])
	}
}

func TestDestinationRingPickValid(t *testing.T) {
	backends := []model.DestinationRef{
		{DestinationID: "a", Weight: 50},
		{DestinationID: "b", Weight: 50},
	}
	ring := buildDestinationRing(backends)

	key := crc32.ChecksumIEEE([]byte("test-session:test-route"))
	original := ring.Pick(key)

	valid := map[string]bool{"a": true, "b": true}
	got := ring.PickValid(key, valid)
	if got != original {
		t.Errorf("PickValid with all valid: expected %s, got %s", original, got)
	}

	onlyOther := map[string]bool{}
	if original == "a" {
		onlyOther["b"] = true
	} else {
		onlyOther["a"] = true
	}
	got = ring.PickValid(key, onlyOther)
	if got == original {
		t.Errorf("PickValid should skip removed destination %s", original)
	}
	if !onlyOther[got] {
		t.Errorf("PickValid returned invalid destination %s", got)
	}
}

func TestDestinationRingDestinationRemoved(t *testing.T) {
	backends := []model.DestinationRef{
		{DestinationID: "a", Weight: 50},
		{DestinationID: "b", Weight: 50},
	}
	ring := buildDestinationRing(backends)

	key := crc32.ChecksumIEEE([]byte("sticky-user:route-1"))
	original := ring.Pick(key)

	valid := map[string]bool{"a": true, "b": true}
	delete(valid, original)

	fallback := ring.PickValid(key, valid)
	if fallback == original {
		t.Error("should have fallen to the other destination")
	}
	if fallback == "" {
		t.Error("should have found a valid destination")
	}
}

func TestDestinationRingEmpty(t *testing.T) {
	ring := buildDestinationRing(nil)
	if got := ring.Pick(12345); got != "" {
		t.Errorf("expected empty, got %s", got)
	}
	if got := ring.PickValid(12345, map[string]bool{"a": true}); got != "" {
		t.Errorf("expected empty, got %s", got)
	}
}

func TestDestinationRingSingleBackend(t *testing.T) {
	ring := buildDestinationRing([]model.DestinationRef{{DestinationID: "only", Weight: 100}})

	for i := 0; i < 100; i++ {
		key := crc32.ChecksumIEEE([]byte(itoa(i)))
		if got := ring.Pick(key); got != "only" {
			t.Fatalf("expected 'only', got %s", got)
		}
	}
}

func TestDestinationRingStableOnWeightChange(t *testing.T) {
	backends1 := []model.DestinationRef{
		{DestinationID: "a", Weight: 80},
		{DestinationID: "b", Weight: 20},
	}
	backends2 := []model.DestinationRef{
		{DestinationID: "a", Weight: 60},
		{DestinationID: "b", Weight: 40},
	}
	ring1 := buildDestinationRing(backends1)
	ring2 := buildDestinationRing(backends2)

	moved := 0
	total := 10000
	for i := 0; i < total; i++ {
		key := crc32.ChecksumIEEE([]byte("user-" + itoa(i)))
		if ring1.Pick(key) != ring2.Pick(key) {
			moved++
		}
	}

	movedPct := float64(moved) / float64(total) * 100
	if movedPct > 30 {
		t.Errorf("too many users moved on weight change: %.1f%%", movedPct)
	}
}
