package main

import (
	"context"
	"sync"
)

// interruptHub implements Ctrl+C semantics for the REPL:
//   - while a request is active: cancel that request and stay in the REPL
//   - while idle at the prompt: request process exit
//
// OS signal delivery is wired in main; this type is unit-tested without signals.
type interruptHub struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	reqID    uint64
	wantExit bool
	wake     chan struct{}
}

func newInterruptHub() *interruptHub {
	return &interruptHub{wake: make(chan struct{}, 1)}
}

// BeginRequest arms cancellation for one in-flight Stream call.
// The returned stop function must be called when the request finishes.
func (h *interruptHub) BeginRequest(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	h.mu.Lock()
	h.reqID++
	id := h.reqID
	h.cancel = cancel
	h.mu.Unlock()
	return ctx, func() {
		cancel()
		h.mu.Lock()
		if h.reqID == id {
			h.cancel = nil
		}
		h.mu.Unlock()
	}
}

// HandleSignal reacts to Ctrl+C. Returns true if the REPL should exit.
func (h *interruptHub) HandleSignal() (exit bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cancel != nil {
		cancel := h.cancel
		h.cancel = nil // allow a second Ctrl+C to request exit while winding down
		cancel()
		return false
	}
	if !h.wantExit {
		h.wantExit = true
		select {
		case h.wake <- struct{}{}:
		default:
		}
	}
	return true
}

// ExitRequested reports whether an idle interrupt asked the process to quit.
func (h *interruptHub) ExitRequested() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.wantExit
}

// Wake returns a channel signaled when an idle interrupt wants exit.
func (h *interruptHub) Wake() <-chan struct{} {
	return h.wake
}
