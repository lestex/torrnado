//go:build !linux && !darwin

package main

import "errors"

// signalStop has nothing to send on a platform with no POSIX signals.
// The daemon does not run there either (see the README's non-goals), so
// this exists to keep the package compiling.
func signalStop(int) error {
	return errors.New("stopping a daemon by signal is not supported on this platform")
}
