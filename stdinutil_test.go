package main

import (
	"errors"
	"io"
	"syscall"
	"testing"
)

func TestIsBenignStdinInterrupt(t *testing.T) {
	if isBenignStdinInterrupt(nil) || isBenignStdinInterrupt(io.EOF) {
		t.Fatal("nil/EOF should not be benign interrupt")
	}
	if !isBenignStdinInterrupt(syscall.EINTR) {
		t.Fatal("EINTR")
	}
	if !isBenignStdinInterrupt(errors.New("read: interrupted system call")) {
		t.Fatal("interrupted system call string")
	}
	if isBenignStdinInterrupt(errors.New("connection reset")) {
		t.Fatal("unrelated error")
	}
}
