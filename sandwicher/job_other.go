//go:build !windows

package sandwicher

// AttachToOwnedJob is a no-op off Windows; process cleanup there relies on
// the child process groups (see main.go signal handling).
func AttachToOwnedJob() error { return nil }
