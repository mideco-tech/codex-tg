package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestVisualMarkerIsStableForSameThread(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()

	first := service.visualMarker(ctx, "thread-stable-marker")
	second := service.visualMarker(ctx, "thread-stable-marker")

	if first == "" || first != second {
		t.Fatalf("visual marker first=%q second=%q, want stable non-empty marker", first, second)
	}
}

func TestVisualMarkerAvoidsActiveCollision(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()

	firstID := "thread-collision-a"
	secondID := findThreadIDWithVisualBase(t, visualHashIndex(firstID, len(visualMarkerPalette)), firstID)

	first := service.visualMarker(ctx, firstID)
	second := service.visualMarker(ctx, secondID)

	if first == second {
		t.Fatalf("visual markers collided for active threads %q and %q: %q", firstID, secondID, first)
	}
}

func TestVisualMarkerUsesSuffixWhenPaletteIsExhausted(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	expires := time.Now().UTC().Add(visualMarkerTTL).Unix()
	for index, marker := range visualMarkerPalette {
		payload, err := json.Marshal(visualMarkerAssignment{Marker: marker, ExpiresAtUnix: expires})
		if err != nil {
			t.Fatalf("marshal assignment failed: %v", err)
		}
		if err := service.store.SetState(ctx, fmt.Sprintf("%sowner-%d", visualMarkerStatePrefix, index), string(payload)); err != nil {
			t.Fatalf("SetState failed: %v", err)
		}
	}

	marker := service.visualMarker(ctx, "thread-overflow-marker")
	if !strings.Contains(marker, "#2") {
		t.Fatalf("overflow marker = %q, want suffixed marker when palette is exhausted", marker)
	}
}

func TestVisualMarkerReusesExpiredAssignment(t *testing.T) {
	t.Parallel()

	service := newTestService(t)
	ctx := context.Background()
	threadID := "thread-expired-marker"
	base := visualMarkerPalette[visualHashIndex(threadID, len(visualMarkerPalette))]
	payload, err := json.Marshal(visualMarkerAssignment{Marker: base, ExpiresAtUnix: time.Now().UTC().Add(-time.Minute).Unix()})
	if err != nil {
		t.Fatalf("marshal assignment failed: %v", err)
	}
	if err := service.store.SetState(ctx, visualMarkerStatePrefix+"expired-owner", string(payload)); err != nil {
		t.Fatalf("SetState failed: %v", err)
	}

	marker := service.visualMarker(ctx, threadID)
	if marker != base {
		t.Fatalf("marker = %q, want expired base marker %q to be reusable", marker, base)
	}
}

func findThreadIDWithVisualBase(t *testing.T, baseIndex int, excluded string) string {
	t.Helper()
	for index := 0; index < 100000; index++ {
		candidate := fmt.Sprintf("thread-collision-candidate-%d", index)
		if candidate == excluded {
			continue
		}
		if visualHashIndex(candidate, len(visualMarkerPalette)) == baseIndex {
			return candidate
		}
	}
	t.Fatalf("could not find visual marker collision candidate for base index %d", baseIndex)
	return ""
}
