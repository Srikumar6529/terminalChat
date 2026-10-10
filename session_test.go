package main

import (
	"errors"
	"testing"

	"terminalchat/anthropic"
)

func TestFinishTurnRollbackOnError(t *testing.T) {
	h := []anthropic.Message{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c"},
	}
	got := finishTurn(h, "partial", errors.New("boom"))
	if len(got) != 2 || got[1].Content != "b" {
		t.Fatalf("got=%+v", got)
	}
}

func TestFinishTurnSuccessAfterPriorFailure(t *testing.T) {
	h := []anthropic.Message{{Role: "user", Content: "first"}}
	h = finishTurn(h, "nope", errors.New("fail"))
	if len(h) != 0 {
		t.Fatalf("after failure: %+v", h)
	}
	h = append(h, anthropic.Message{Role: "user", Content: "second"})
	h = finishTurn(h, "ok", nil)
	if len(h) != 2 || h[0].Content != "second" || h[1].Content != "ok" {
		t.Fatalf("after success: %+v", h)
	}
}

func TestFinishTurnEmptySuccess(t *testing.T) {
	h := []anthropic.Message{{Role: "user", Content: "q"}}
	got := finishTurn(h, "", nil)
	if len(got) != 1 {
		t.Fatalf("empty assistant should not append: %+v", got)
	}
}
