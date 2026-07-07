package logging

import (
	"io"
	"log"
	"testing"
)

func TestAcquireLoggerCallback_NilLogger(t *testing.T) {
	slotIdx, callback := AcquireLoggerCallback(nil, uint8(WarnAsInt))
	if slotIdx != -1 {
		t.Fatalf("expected slot index -1, got %d", slotIdx)
	}

	if callback != 0 {
		t.Fatalf("expected callback 0, got %d", callback)
	}
}

func TestAcquireLoggerCallback_ReusesSlots(t *testing.T) {
	baseline := CallbackSlotCount()
	logger := log.New(io.Discard, "", 0)

	for i := 0; i < 1000; i++ {
		slotIdx, callback := AcquireLoggerCallback(logger, uint8(DebugAsInt))
		if slotIdx < 0 {
			t.Fatalf("expected non-negative slot index, got %d", slotIdx)
		}
		if callback == 0 {
			t.Fatal("expected callback to be non-zero")
		}

		ReleaseLoggerCallbackSlot(slotIdx)
	}

	after := CallbackSlotCount()
	if after > baseline+1 {
		t.Fatalf("expected bounded slot count, baseline=%d after=%d", baseline, after)
	}
}

func TestAcquireLoggerCallback_BoundsToPeakConcurrency(t *testing.T) {
	baseline := CallbackSlotCount()
	logger := log.New(io.Discard, "", 0)
	const peak = 12

	slots := make([]int, 0, peak)
	for i := 0; i < peak; i++ {
		slotIdx, callback := AcquireLoggerCallback(logger, uint8(DebugAsInt))
		if slotIdx < 0 {
			t.Fatalf("expected non-negative slot index, got %d", slotIdx)
		}
		if callback == 0 {
			t.Fatal("expected callback to be non-zero")
		}

		slots = append(slots, slotIdx)
	}

	for _, slotIdx := range slots {
		ReleaseLoggerCallbackSlot(slotIdx)
	}

	after := CallbackSlotCount()
	if after > baseline+peak {
		t.Fatalf("expected slot count <= baseline+peak, baseline=%d peak=%d after=%d", baseline, peak, after)
	}
}
