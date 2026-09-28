package internal

import (
	"io"
	"log"
	"testing"
	"unsafe"

	scrapligologging "github.com/kentik/scrapligo/v2/logging"
)

// applyToFreshOptions mimics what Cli.Open/Netconf.Open do -- apply the options onto a zeroed
// driver options struct. libscrapli's ls_alloc_driver_options default initializes the struct it
// hands back, so a zero value here is a faithful stand in and lets us assert on the applied
// struct without needing libscrapli itself.
func applyToFreshOptions(t *testing.T, o *Options) *driverOptions {
	t.Helper()

	opts := &driverOptions{}

	err := o.Apply(
		GetUserDataDispatcherr().Register(),
		uintptr(unsafe.Pointer(opts)),
	)
	if err != nil {
		t.Fatalf("unexpected error applying options: %s", err)
	}

	return opts
}

// TestApplyOmitsLoggerCallbackWithoutLogger asserts we do *not* hand libscrapli a logger callback
// when the user never configured a logger. libscrapli invokes the logger callback from its own
// (non go) threads, so an always-set callback means every log line libscrapli emits becomes a
// foreign thread -> go runtime transition, even when there is no logger to dispatch to.
func TestApplyOmitsLoggerCallbackWithoutLogger(t *testing.T) {
	o := NewOptions()

	opts := applyToFreshOptions(t, o)

	if opts.loggerCallback != 0 {
		t.Fatalf(
			"expected no logger callback to be set when no logger is configured, got %d",
			opts.loggerCallback,
		)
	}

	if opts.loggerLevel != ffiLoggerLevelWarn {
		t.Fatalf(
			"expected logger level %d to still be applied, got %d",
			ffiLoggerLevelWarn,
			opts.loggerLevel,
		)
	}
}

// TestApplySetsLoggerCallbackWithLogger asserts the callback *is* set (and the logger registered
// for dispatch) when the user actually configured a logger.
func TestApplySetsLoggerCallbackWithLogger(t *testing.T) {
	o := NewOptions()
	o.Logger = log.New(io.Discard, "", 0)
	o.LoggerLevel = scrapligologging.Debug

	opts := applyToFreshOptions(t, o)

	if opts.loggerCallback == 0 {
		t.Fatal("expected logger callback to be set when a logger is configured")
	}

	if opts.loggerLevel != ffiLoggerLevelDebug {
		t.Fatalf("expected logger level %d, got %d", ffiLoggerLevelDebug, opts.loggerLevel)
	}

	ld := GetLoggerDispatcher()

	defer ld.Deregister(opts.userData)

	loggingDispatcherInst.lock.RLock()
	_, ok := loggingDispatcherInst.loggers[opts.userData]
	loggingDispatcherInst.lock.RUnlock()

	if !ok {
		t.Fatal("expected logger to be registered with the logger dispatcher")
	}
}

// TestApplyRejectsInvalidLogger asserts that an unsupported logger type is still rejected -- the
// validation lives in the dispatcher's Register, which we now only reach when a logger is set.
func TestApplyRejectsInvalidLogger(t *testing.T) {
	o := NewOptions()
	o.Logger = "not a logger"

	opts := &driverOptions{}

	err := o.Apply(
		GetUserDataDispatcherr().Register(),
		uintptr(unsafe.Pointer(opts)),
	)
	if err == nil {
		t.Fatal("expected an error applying options with an invalid logger type, got none")
	}
}
