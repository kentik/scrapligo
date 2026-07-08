package logging_test

import (
	"io"
	"log"
	"testing"

	scrapligologging "github.com/kentik/scrapligo/v2/logging"
)

func TestAcquireLoggerCallback_NilLogger(t *testing.T) {
	slotIdx, callback := scrapligologging.AcquireLoggerCallback(
		nil,
		uint8(scrapligologging.WarnAsInt),
	)
	if slotIdx != -1 {
		t.Fatalf("expected slot index -1, got %d", slotIdx)
	}

	if callback != 0 {
		t.Fatalf("expected callback 0, got %d", callback)
	}
}

func TestAcquireLoggerCallback_ReusesSlots(t *testing.T) {
	baseline := scrapligologging.CallbackSlotCount()
	logger := log.New(io.Discard, "", 0)

	for range 1000 {
		slotIdx, callback := scrapligologging.AcquireLoggerCallback(
			logger,
			uint8(scrapligologging.DebugAsInt),
		)
		if slotIdx < 0 {
			t.Fatalf("expected non-negative slot index, got %d", slotIdx)
		}

		if callback == 0 {
			t.Fatal("expected callback to be non-zero")
		}

		scrapligologging.ReleaseLoggerCallbackSlot(slotIdx)
	}

	after := scrapligologging.CallbackSlotCount()
	if after > baseline+1 {
		t.Fatalf("expected bounded slot count, baseline=%d after=%d", baseline, after)
	}
}

func TestAcquireLoggerCallback_BoundsToPeakConcurrency(t *testing.T) {
	baseline := scrapligologging.CallbackSlotCount()
	logger := log.New(io.Discard, "", 0)

	const peak = 12

	slots := make([]int, 0, peak)

	for range peak {
		slotIdx, callback := scrapligologging.AcquireLoggerCallback(
			logger,
			uint8(scrapligologging.DebugAsInt),
		)
		if slotIdx < 0 {
			t.Fatalf("expected non-negative slot index, got %d", slotIdx)
		}

		if callback == 0 {
			t.Fatal("expected callback to be non-zero")
		}

		slots = append(slots, slotIdx)
	}

	for _, slotIdx := range slots {
		scrapligologging.ReleaseLoggerCallbackSlot(slotIdx)
	}

	after := scrapligologging.CallbackSlotCount()
	if after > baseline+peak {
		t.Fatalf(
			"expected slot count <= baseline+peak, baseline=%d peak=%d after=%d",
			baseline,
			peak,
			after,
		)
	}
}
