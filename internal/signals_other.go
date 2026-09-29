//go:build !linux || !(amd64 || arm64)

package internal

// EnsureSignalHandlersOnStack is a no-op outside linux amd64/arm64; libscrapli itself sets
// SA_ONSTACK on its handlers.
func EnsureSignalHandlersOnStack() {}
