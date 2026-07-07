package logging

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego"
	scrapligoconstants "github.com/kentik/scrapligo/v2/constants"
)

var (
	// Level is the level at which to emit log messages. When the ScrapligoDebug env var is set
	// we use that value (if it == one of the log levels, otherwise it defaults to debug), for all
	// other cases this defaults to warn.
	Level LogLevel //nolint: gochecknoglobals

	// Logger is the main logging function, used mostly for "global" non connection/device related
	// things like the ffi layer.
	Logger = func(level LogLevel, m string, a ...any) { //nolint: gochecknoglobals
		if IntFromLevel(Level) <= IntFromLevel(level) {
			_, _ = fmt.Fprintln(os.Stderr, level, "::", fmt.Sprintf(m, a...))
		}
	}

	loggerSlotsMu sync.Mutex //nolint: gochecknoglobals
	loggerSlots   []*loggerSlot
)

type loggerState struct {
	logger   any
	logLevel uint8
}

type loggerSlot struct {
	callback uintptr
	inUse    atomic.Bool
	state    atomic.Pointer[loggerState]
}

// AcquireLoggerCallback acquires a reusable callback slot for the given logger.
func AcquireLoggerCallback(logger any, logLevel uint8) (int, uintptr) {
	if logger == nil {
		return -1, 0
	}

	loggerSlotsMu.Lock()
	defer loggerSlotsMu.Unlock()

	for idx, slot := range loggerSlots {
		if !slot.inUse.CompareAndSwap(false, true) {
			continue
		}

		slot.state.Store(&loggerState{logger: logger, logLevel: logLevel})

		return idx, slot.callback
	}

	slot := &loggerSlot{}
	slot.inUse.Store(true)
	slot.state.Store(&loggerState{logger: logger, logLevel: logLevel})
	slot.callback = purego.NewCallback(func(level uint8, message *string) {
		state := slot.state.Load()
		if state == nil {
			return
		}

		dispatchLog(state, level, message)
	})

	loggerSlots = append(loggerSlots, slot)

	return len(loggerSlots) - 1, slot.callback
}

// ReleaseLoggerCallbackSlot releases a callback slot for reuse.
func ReleaseLoggerCallbackSlot(slotIdx int) {
	if slotIdx < 0 {
		return
	}

	loggerSlotsMu.Lock()
	defer loggerSlotsMu.Unlock()

	if slotIdx >= len(loggerSlots) {
		return
	}

	slot := loggerSlots[slotIdx]
	slot.state.Store(nil)
	slot.inUse.Store(false)
}

func dispatchLog(state *loggerState, level uint8, message *string) { //nolint: gocyclo
	if state.logLevel > level || message == nil {
		return
	}

	switch l := state.logger.(type) {
	case *log.Logger:
		switch level {
		case uint8(TraceAsInt):
			l.Printf("trace :: %s", *message)
		case uint8(DebugAsInt):
			l.Printf("debug :: %s", *message)
		case uint8(InfoAsInt):
			l.Printf(" info :: %s", *message)
		case uint8(WarnAsInt):
			l.Printf(" warn :: %s", *message)
		case uint8(CriticalAsInt):
			l.Printf(" crit :: %s", *message)
		case uint8(FatalAsInt):
			l.Printf("fatal :: %s", *message)
		case uint8(DisabledAsInt):
		}
	case *slog.Logger:
		// ignoring context things since we (currently?) expose no means to actually pass
		// a context with things here anyway
		switch level {
		case uint8(TraceAsInt):
			// no "trace" level, so... just debug it and add the trace prefix for clarity
			l.Debug(fmt.Sprintf("trace: %s", *message))
		case uint8(DebugAsInt):
			l.Debug(*message)
		case uint8(InfoAsInt):
			l.Info(*message)
		case uint8(WarnAsInt):
			l.Warn(*message)
		case uint8(CriticalAsInt):
			l.Error(*message)
		case uint8(FatalAsInt):
			l.Error(*message)
		case uint8(DisabledAsInt):
		}
	case func(LogLevel, string):
		l(LevelFromInt(level), *message)
	default:
	}
}

// CallbackSlotCount returns the number of allocated callback slots.
func CallbackSlotCount() int {
	loggerSlotsMu.Lock()
	defer loggerSlotsMu.Unlock()

	return len(loggerSlots)
}

// normally i *really* dislike inits but... meh?
func init() { //nolint: gochecknoinits
	v := os.Getenv(scrapligoconstants.ScrapligoDebug)

	if v != "" {
		switch v {
		case Trace.String():
			Level = Trace
		case Debug.String():
			Level = Debug
		case Info.String():
			Level = Info
		case Warn.String():
			Level = Warn
		case Critical.String():
			Level = Critical
		case Fatal.String():
			Level = Fatal
		case Disabled.String():
			Level = Disabled
		default:
			Level = Debug
		}
	} else {
		Level = Warn
	}
}
