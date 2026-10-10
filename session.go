package main

import "terminalchat/anthropic"

// finishTurn updates history after a Stream attempt.
// On any error (including cancellation), the trailing user turn is rolled back
// and no assistant message is recorded — even if partial text was printed.
func finishTurn(history []anthropic.Message, text string, err error) []anthropic.Message {
	if err != nil {
		return rollbackLastUser(history)
	}
	return appendAssistant(history, text)
}
