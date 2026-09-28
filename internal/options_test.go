package internal_test

import (
	"io"
	"log"
	"testing"

	scrapligointernal "github.com/kentik/scrapligo/v2/internal"
	scrapligologging "github.com/kentik/scrapligo/v2/logging"
)

// TestRegisterLoggerWithoutLogger asserts we do *not* hand libscrapli a logger callback when the
// user never configured a logger. libscrapli invokes the logger callback from its own (non go)
// threads, so an always-set callback means every log line libscrapli emits becomes a foreign
// thread -> go runtime transition, even when there is nothing to dispatch to.
func TestRegisterLoggerWithoutLogger(t *testing.T) {
	o := scrapligointernal.NewOptions()

	cb, err := o.RegisterLogger(scrapligointernal.GetUserDataDispatcherr().Register())
	if err != nil {
		t.Fatalf("unexpected error registering logger: %s", err)
	}

	if cb != 0 {
		t.Fatalf("expected no logger callback when no logger is configured, got %d", cb)
	}
}

// TestRegisterLoggerWithLogger asserts the callback *is* returned when the user actually
// configured a logger, and that it is the dispatcher's shared callback.
func TestRegisterLoggerWithLogger(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		logger any
	}{
		{name: "log-logger", logger: log.New(io.Discard, "", 0)},
		{
			name:   "func-logger",
			logger: func(_ scrapligologging.LogLevel, _ string) {},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Logf("%s: starting", testCase.name)

			o := scrapligointernal.NewOptions()
			o.Logger = testCase.logger
			o.LoggerLevel = scrapligologging.Debug

			userData := scrapligointernal.GetUserDataDispatcherr().Register()

			defer scrapligointernal.GetLoggerDispatcher().Deregister(userData)

			cb, err := o.RegisterLogger(userData)
			if err != nil {
				t.Fatalf("unexpected error registering logger: %s", err)
			}

			if cb == 0 {
				t.Fatal("expected a logger callback when a logger is configured")
			}

			expected := scrapligointernal.GetLoggerDispatcher().GetLoggerCallback()
			if cb != expected {
				t.Fatalf("expected dispatcher callback %d, got %d", expected, cb)
			}
		})
	}
}

// TestRegisterLoggerInvalidLogger asserts that an unsupported logger type is still rejected -- the
// validation lives in the dispatcher's Register, which we now only reach when a logger is set.
func TestRegisterLoggerInvalidLogger(t *testing.T) {
	o := scrapligointernal.NewOptions()
	o.Logger = "not a logger"

	cb, err := o.RegisterLogger(scrapligointernal.GetUserDataDispatcherr().Register())
	if err == nil {
		t.Fatal("expected an error registering an invalid logger type, got none")
	}

	if cb != 0 {
		t.Fatalf("expected no logger callback when registration fails, got %d", cb)
	}
}
