package main

import (
	"errors"
	"io"
	"strings"
	"syscall"
)

// isBenignStdinInterrupt reports read errors that commonly accompany Ctrl+C
// on a blocking stdin read. These should not force a non-zero process exit
// when the interrupt hub already requested a clean shutdown.
func isBenignStdinInterrupt(err error) bool {
	if err == nil || errors.Is(err, io.EOF) {
		return false
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == syscall.EINTR {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "interrupted") || strings.Contains(msg, "eintr")
}
