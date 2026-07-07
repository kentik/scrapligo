package internal

import (
	"testing"
	"unsafe"

	scrapligologging "github.com/kentik/scrapligo/v2/logging"
)

func TestOptionsApply_CallbackSlotsBoundedAndReusable(t *testing.T) {
	baselineLoggerSlots := scrapligologging.CallbackSlotCount()
	baselineCapabilitiesSlots := len(capabilitiesCallbackSlots)
	baselineRecorderSlots := len(recorderCallbackSlots)

	o := NewOptions()
	o.Logger = func(_ scrapligologging.LogLevel, _ string) {}
	o.Netconf.CapabilitiesCallback = func(serverCapabilities *string) *string {
		return serverCapabilities
	}
	o.Session.RecorderCallback = func(_ *[]byte) {}

	var firstApplyOpts driverOptions
	o.Apply(uintptr(unsafe.Pointer(&firstApplyOpts)))

	if o.loggerCallbackSlot < 0 || firstApplyOpts.loggerCallback == 0 {
		t.Fatal("expected logger callback slot to be acquired")
	}
	if o.Netconf.capabilitiesCallbackSlot < 0 || firstApplyOpts.netconf.capabilitiesCallback == 0 {
		t.Fatal("expected capabilities callback slot to be acquired")
	}
	if o.Session.recorderCallbackSlot < 0 || firstApplyOpts.session.recorderCallback == 0 {
		t.Fatal("expected recorder callback slot to be acquired")
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

	var secondApplyOpts driverOptions
	o.Apply(uintptr(unsafe.Pointer(&secondApplyOpts)))
	o.ReleaseCallbackSlots()

	afterLoggerSlots := scrapligologging.CallbackSlotCount()
	afterCapabilitiesSlots := len(capabilitiesCallbackSlots)
	afterRecorderSlots := len(recorderCallbackSlots)

	if afterLoggerSlots > baselineLoggerSlots+1 {
		t.Fatalf("expected bounded logger slot growth, baseline=%d after=%d", baselineLoggerSlots, afterLoggerSlots)
	}
	if afterCapabilitiesSlots > baselineCapabilitiesSlots+1 {
		t.Fatalf(
			"expected bounded capabilities slot growth, baseline=%d after=%d",
			baselineCapabilitiesSlots,
			afterCapabilitiesSlots,
		)
	}
	if afterRecorderSlots > baselineRecorderSlots+1 {
		t.Fatalf(
			"expected bounded recorder slot growth, baseline=%d after=%d",
			baselineRecorderSlots,
			afterRecorderSlots,
		)
	}
}
