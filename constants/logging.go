package constants

const (
	// ScrapligoDebug is the env var when if set to anything enables debug logging -- this is
	// almost entirely for ffi related bits as all other logging would be handled by providing
	// a logger callback to libscrapli.
	ScrapligoDebug = "SCRAPLIGO_DEBUG"

	// ScrapligoFfiGuard is the env var that, when set to a non-empty value, enables ffi buffer
	// guarding. Guarding asserts that libscrapli only ever writes *into* the buffers we lend it
	// and never over the go slice headers describing them -- see internal.FfiBufferGuards. It is
	// opt-in because it converts what would otherwise be silent memory corruption into a returned
	// error, which is a behaviour change, but it is cheap enough to leave on for canary runs.
	ScrapligoFfiGuard = "SCRAPLIGO_FFI_GUARD"
)
