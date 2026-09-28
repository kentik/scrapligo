package internal_test

import (
	"strings"
	"testing"

	scrapligoconstants "github.com/kentik/scrapligo/v2/constants"
	scrapligointernal "github.com/kentik/scrapligo/v2/internal"
)

// TestFfiBufferGuardsDisabledByDefault asserts guarding is opt-in and that a nil guards value is
// a usable do-nothing receiver -- callers must not need a nil check at every call site.
func TestFfiBufferGuardsDisabledByDefault(t *testing.T) {
	// explicitly pin the env to "unset" so the test is deterministic regardless of what the
	// surrounding environment (or a canary run) has configured
	t.Setenv(scrapligoconstants.ScrapligoFfiGuard, "")

	if scrapligointernal.FfiGuardEnabled() {
		t.Fatal("expected ffi guarding to be disabled when the env var is unset")
	}

	guards := scrapligointernal.NewFfiBufferGuards()
	if guards != nil {
		t.Fatal("expected nil guards when ffi guarding is disabled")
	}

	buf := make([]byte, 8)

	scrapligointernal.WatchFfiBuffer(guards, "buf", &buf)

	// simulate a header write -- with guarding off this must still be a no-op rather than a panic
	buf = nil

	_ = buf

	err := guards.Verify()
	if err != nil {
		t.Fatalf("expected nil guards to verify clean, got %s", err)
	}
}

// TestFfiBufferGuardsUntouchedBuffer asserts we do not cry wolf: libscrapli writing *into* a
// buffer (which is the whole point of lending it) must not trip the guard.
func TestFfiBufferGuardsUntouchedBuffer(t *testing.T) {
	t.Setenv(scrapligoconstants.ScrapligoFfiGuard, "1")

	buf := make([]byte, 8)
	lens := make([]uint64, 2)

	guards := scrapligointernal.NewFfiBufferGuards()
	if guards == nil {
		t.Fatal("expected non-nil guards when ffi guarding is enabled")
	}

	scrapligointernal.WatchFfiBuffer(guards, "buf", &buf)
	scrapligointernal.WatchFfiBuffer(guards, "lens", &lens)

	// writing into the buffers is exactly what libscrapli is supposed to do
	copy(buf, "abcdefgh")

	lens[0] = 8

	err := guards.Verify()
	if err != nil {
		t.Fatalf("expected writes into a guarded buffer to verify clean, got %s", err)
	}
}

// TestFfiBufferGuardsDetectsHeaderWrite reproduces the shape of the libscrapli defect this guard
// exists to catch: zig assigning a whole slice through the caller's `*[]u8` (i.e.
// `operation_error.* = "";`) rather than copying into the buffer it points at. That swaps our
// data pointer for one of libscrapli's and zeroes our length.
func TestFfiBufferGuardsDetectsHeaderWrite(t *testing.T) {
	t.Setenv(scrapligoconstants.ScrapligoFfiGuard, "1")

	errString := make([]byte, 16)

	guards := scrapligointernal.NewFfiBufferGuards()

	scrapligointernal.WatchFfiBuffer(guards, "errString", &errString)

	// stand-in for libscrapli's `operation_error.* = ""` -- a foreign data pointer and a zeroed
	// length written over our header
	foreign := make([]byte, 0)

	errString = foreign

	err := guards.Verify()
	if err == nil {
		t.Fatal("expected guard to detect the slice header being overwritten")
	}

	if !strings.Contains(err.Error(), "errString") {
		t.Fatalf("expected the violation to name the offending buffer, got %s", err)
	}
}

// TestFfiBufferGuardsDetectsLengthOnlyWrite asserts we catch a header write even when the data
// pointer happens to survive -- a truncated length silently discards everything libscrapli was
// meant to hand back.
func TestFfiBufferGuardsDetectsLengthOnlyWrite(t *testing.T) {
	t.Setenv(scrapligoconstants.ScrapligoFfiGuard, "1")

	buf := make([]byte, 16)

	guards := scrapligointernal.NewFfiBufferGuards()

	scrapligointernal.WatchFfiBuffer(guards, "buf", &buf)

	buf = buf[:0]

	err := guards.Verify()
	if err == nil {
		t.Fatal("expected guard to detect the slice length being changed")
	}
}
