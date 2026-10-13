package memory

import "log/slog"

// testBridgeSlog returns a quiet logger for bridge tests.
func testBridgeSlog() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}
