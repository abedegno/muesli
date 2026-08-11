//go:build !windows

package embedded

// embeddedPostgresSupported reports whether this platform can run the embedded
// server. See platform_windows.go for why Windows is excluded.
const embeddedPostgresSupported = true
