package main

import "terminalchat/anthropic"

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

// appendAssistant adds a completed assistant turn. Empty text is ignored so a
// successful but empty completion does not invent a blank history entry.
func appendAssistant(history []anthropic.Message, text string) []anthropic.Message {
	if text == "" {
		return history
	}
	return append(history, anthropic.Message{Role: "assistant", Content: text})
}
