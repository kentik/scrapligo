package internal //nolint:testpackage // white-box test needs access to unexported slot pools

import (
	"testing"

	scrapligologging "github.com/kentik/scrapligo/v2/logging"
)

func TestNetconfOptions_CapabilitiesCallbackSlotReuse(t *testing.T) {
	baseline := len(capabilitiesCallbackSlots)

	o := &NetconfOptions{capabilitiesCallbackSlot: -1}
	o.CapabilitiesCallback = func(serverCapabilities *string) *string {
		return serverCapabilities
	}

	for range 100 {
		var applied driverOptions

		o.apply(&applied)

		if o.capabilitiesCallbackSlot < 0 || applied.netconf.capabilitiesCallback == 0 {
			t.Fatal("expected capabilities callback slot to be acquired")
		}

		o.releaseCapabilitiesCallbackSlot()

		if o.capabilitiesCallbackSlot != -1 || o.capabilitiesCallback != 0 {
			t.Fatal("expected capabilities callback slot to be released")
		}
	}

	if got := len(capabilitiesCallbackSlots); got > baseline+1 {
		t.Fatalf("expected bounded capabilities slot growth, baseline=%d after=%d", baseline, got)
	}
}

func TestSessionOptions_RecorderCallbackSlotReuse(t *testing.T) {
	baseline := len(recorderCallbackSlots)

	o := &SessionOptions{recorderCallbackSlot: -1}
	o.RecorderCallback = func(_ *[]byte) {}

	for range 100 {
		var applied driverOptions

		o.apply(&applied)

		if o.recorderCallbackSlot < 0 || applied.session.recorderCallback == 0 {
			t.Fatal("expected recorder callback slot to be acquired")
		}

		o.releaseRecorderCallbackSlot()

		if o.recorderCallbackSlot != -1 || o.recorderCallback != 0 {
			t.Fatal("expected recorder callback slot to be released")
		}
	}

	if got := len(recorderCallbackSlots); got > baseline+1 {
		t.Fatalf("expected bounded recorder slot growth, baseline=%d after=%d", baseline, got)
	}
}

func TestOptions_ReleaseCallbackSlotsReleasesAll(t *testing.T) {
	o := NewOptions()
	o.loggerCallbackSlot, o.loggerCallback = scrapligologging.AcquireLoggerCallback(
		func(_ scrapligologging.LogLevel, _ string) {},
		uint8(scrapligologging.DebugAsInt),
	)
	o.Netconf.CapabilitiesCallback = func(serverCapabilities *string) *string {
		return serverCapabilities
	}
	o.Session.RecorderCallback = func(_ *[]byte) {}

	var applied driverOptions

	o.Netconf.apply(&applied)
	o.Session.apply(&applied)

	if o.loggerCallbackSlot < 0 ||
		o.Netconf.capabilitiesCallbackSlot < 0 ||
		o.Session.recorderCallbackSlot < 0 {
		t.Fatal("expected all callback slots to be acquired")
	}

	o.ReleaseCallbackSlots()

	if o.loggerCallbackSlot != -1 || o.loggerCallback != 0 {
		t.Fatal("expected logger callback slot to be released")
	}

	if o.Netconf.capabilitiesCallbackSlot != -1 || o.Netconf.capabilitiesCallback != 0 {
		t.Fatal("expected capabilities callback slot to be released")
	}

	if o.Session.recorderCallbackSlot != -1 || o.Session.recorderCallback != 0 {
		t.Fatal("expected recorder callback slot to be released")
	}
}
