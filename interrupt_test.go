package main

import (
	"context"
	"testing"
	"time"
)

func TestInterruptCancelsActiveRequest(t *testing.T) {
	h := newInterruptHub()
	ctx, stop := h.BeginRequest(context.Background())
	defer stop()

	if exit := h.HandleSignal(); exit {
		t.Fatal("expected cancel, not exit, while streaming")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("request context was not canceled")
	}
	if h.ExitRequested() {
		t.Fatal("exit should not be requested after canceling a stream")
	}
}

func TestInterruptAtIdleRequestsExit(t *testing.T) {
	h := newInterruptHub()
	if exit := h.HandleSignal(); !exit {
		t.Fatal("expected exit when idle")
	}
	if !h.ExitRequested() {
		t.Fatal("ExitRequested = false")
	}
	select {
	case <-h.Wake():
	default:
		t.Fatal("wake was not signaled")
	}
}

func TestInterruptAfterRequestFinishedExits(t *testing.T) {
	h := newInterruptHub()
	_, stop := h.BeginRequest(context.Background())
	stop()

	if exit := h.HandleSignal(); !exit {
		t.Fatal("expected exit when idle after request")
	}
}

func TestSecondSignalAfterCancelStillExits(t *testing.T) {
	h := newInterruptHub()
	ctx, stop := h.BeginRequest(context.Background())
	if h.HandleSignal() {
		t.Fatal("first signal should cancel")
	}
	<-ctx.Done()
	// Second Ctrl+C before stop() should still request exit.
	if !h.HandleSignal() {
		t.Fatal("second signal should exit while request winds down")
	}
	stop()
	if !h.ExitRequested() {
		t.Fatal("ExitRequested = false")
	}
}

func TestBeginRequestAllowsSubsequentRequest(t *testing.T) {
	h := newInterruptHub()
	ctx1, stop1 := h.BeginRequest(context.Background())
	h.HandleSignal()
	<-ctx1.Done()
	stop1()

	ctx2, stop2 := h.BeginRequest(context.Background())
	defer stop2()
	select {
	case <-ctx2.Done():
		t.Fatal("new request should not start canceled")
	default:
	}
}
