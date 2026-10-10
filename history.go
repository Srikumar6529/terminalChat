package main

import "terminalchat/anthropic"

// maxHistoryMessages caps in-memory turns so request size and RAM stay bounded.
// Oldest messages are dropped; trimming tries to keep the slice starting on a user turn.
const maxHistoryMessages = 40

// rollbackLastUser drops the trailing user turn after a failed or canceled request.
func rollbackLastUser(history []anthropic.Message) []anthropic.Message {
	if len(history) == 0 {
		return history
	}
	if history[len(history)-1].Role != "user" {
		return history
	}
	return history[:len(history)-1]
}

// appendAssistant adds a completed assistant turn.
// Empty text still records an assistant message so role alternation stays valid.
func appendAssistant(history []anthropic.Message, text string) []anthropic.Message {
	return append(history, anthropic.Message{Role: "assistant", Content: text})
}

// trimHistory drops the oldest messages when the transcript exceeds maxHistoryMessages.
func trimHistory(history []anthropic.Message) []anthropic.Message {
	if len(history) <= maxHistoryMessages {
		return history
	}
	history = history[len(history)-maxHistoryMessages:]
	for len(history) > 0 && history[0].Role != "user" {
		history = history[1:]
	}
	return history
}
