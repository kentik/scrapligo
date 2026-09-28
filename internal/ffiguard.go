package internal

import (
	"fmt"
	"os"
	"unsafe"

	scrapligoconstants "github.com/kentik/scrapligo/v2/constants"
	scrapligoerrors "github.com/kentik/scrapligo/v2/errors"
)

// FfiGuardEnabled reports whether ffi buffer guarding is enabled via the
// constants.ScrapligoFfiGuard env var. Guarding is opt-in: it costs a handful of word comparisons
// per operation, but it turns an otherwise silent, unattributable memory corruption into an
// immediate error naming the exact buffer that was violated, so it is well worth switching on for
// canary/diagnostic runs. As with constants.ScrapligoDebug an empty value counts as unset.
func FfiGuardEnabled() bool {
	return os.Getenv(scrapligoconstants.ScrapligoFfiGuard) != ""
}

// FfiBufferGuards collects checks that assert libscrapli only ever wrote *into* the buffers we
// lend it and never over the slice headers describing them.
//
// This matters because of a header layout mismatch across the boundary: a go []byte header is
// three words (data/len/cap) while the zig []u8 libscrapli receives is two (ptr/len). We hand
// libscrapli the address of our header, so reads line up fine -- but any *assignment* through
// that pointer on the zig side overwrites our data pointer with one of libscrapli's own addresses
// and clobbers our length. That plants a foreign pointer inside memory the go runtime owns and
// manages, and silently truncates whatever the buffer was supposed to carry back. A nil
// FfiBufferGuards is valid and does nothing, so callers can leave guarding switched off cheaply.
type FfiBufferGuards struct {
	checks []func() error
}

// NewFfiBufferGuards returns guards if guarding is enabled, otherwise nil (which is a valid,
// do-nothing receiver).
func NewFfiBufferGuards() *FfiBufferGuards {
	if !FfiGuardEnabled() {
		return nil
	}

	return &FfiBufferGuards{}
}

// WatchFfiBuffer records the current header of the slice at s so a later Verify can detect
// libscrapli having written over the header rather than into the buffer it points at.
//
// Note this deliberately takes a *pointer to the slice* rather than the slice itself: the pointer
// we record is the very same one we hand across the ffi boundary, so watching through it is what
// lets us observe a write to the header.
func WatchFfiBuffer[T any](g *FfiBufferGuards, name string, s *[]T) {
	if g == nil || s == nil {
		return
	}

	var (
		data = sliceDataAddr(*s)
		l    = len(*s)
		c    = cap(*s)
	)

	g.checks = append(g.checks, func() error {
		gotData, gotLen, gotCap := sliceDataAddr(*s), len(*s), cap(*s)

		if gotData == data && gotLen == l && gotCap == c {
			return nil
		}

		return scrapligoerrors.NewFfiError(
			fmt.Sprintf(
				"libscrapli wrote over the go slice header for ffi buffer %q "+
					"(data 0x%x->0x%x, len %d->%d, cap %d->%d); "+
					"this is memory corruption of go owned memory, not a normal ffi failure",
				name, data, gotData, l, gotLen, c, gotCap,
			),
			nil,
		)
	})
}

// Verify runs every recorded check, returning the first violation found.
func (g *FfiBufferGuards) Verify() error {
	if g == nil {
		return nil
	}

	for _, check := range g.checks {
		err := check()
		if err != nil {
			return err
		}
	}

	return nil
}

func sliceDataAddr[T any](s []T) uintptr {
	return uintptr(unsafe.Pointer(unsafe.SliceData(s))) //nolint: gosec
}
