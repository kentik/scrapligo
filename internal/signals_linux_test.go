//go:build linux && (amd64 || arm64)

package internal //nolint: testpackage

import (
	"syscall"
	"testing"
)

func TestEnsureSignalHandlersOnStack(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGPIPE, syscall.SIGIO} {
		t.Run(sig.String(), func(t *testing.T) {
			orig, err := getSigaction(sig)
			if err != nil {
				t.Fatalf("failed reading sigaction: %s", err)
			}

			t.Cleanup(func() {
				_ = setSigaction(sig, &orig)
			})

			if orig.handler == sigDfl || orig.handler == sigIgn {
				t.Skipf("no handler installed for %s", sig)
			}

			// simulate a foreign (e.g. zig) handler installed without SA_ONSTACK
			stripped := orig
			stripped.flags &^= saOnStack

			err = setSigaction(sig, &stripped)
			if err != nil {
				t.Fatalf("failed writing sigaction: %s", err)
			}

			EnsureSignalHandlersOnStack()

			got, err := getSigaction(sig)
			if err != nil {
				t.Fatalf("failed reading sigaction: %s", err)
			}

			if got.flags&saOnStack == 0 {
				t.Fatalf("expected SA_ONSTACK to be set, flags: %#x", got.flags)
			}

			if got.handler != orig.handler || got.restorer != orig.restorer ||
				got.mask != orig.mask || got.flags != orig.flags|saOnStack {
				t.Fatalf("expected only SA_ONSTACK to change, got %+v, orig %+v", got, orig)
			}
		})
	}
}
