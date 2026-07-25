package handler

// ResetShutdown clears what BeginShutdown sets. Only the test binary sees this,
// so the package's own API stays activate-only.
func ResetShutdown() {
	isShuttingDown.Store(false)
}
