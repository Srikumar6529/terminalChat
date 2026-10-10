package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ErrIncompleteStream is returned when the SSE body ends before a message_stop event.
var ErrIncompleteStream = errors.New("anthropic: incomplete stream: missing message_stop")

const (
	DefaultBaseURL = "https://api.anthropic.com"
	APIVersion     = "2023-06-01"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Stream    bool      `json:"stream"`
	System    string    `json:"system,omitempty"`
	Messages  []Message `json:"messages"`
}

type APIError struct {
	StatusCode int
	Type       string
	Message    string
}

func (e *APIError) Error() string {
	if e == nil {
		return "anthropic: <nil>"
	}
	msg := e.Message
	if msg == "" {
		msg = "unknown error"
	}
	switch {
	case e.Type != "" && e.StatusCode != 0:
		return fmt.Sprintf("anthropic: %s (%s, HTTP %d)", msg, e.Type, e.StatusCode)
	case e.Type != "":
		return fmt.Sprintf("anthropic: %s (%s)", msg, e.Type)
	case e.StatusCode != 0:
		return fmt.Sprintf("anthropic: %s (HTTP %d)", msg, e.StatusCode)
	default:
		return "anthropic: " + msg
	}
}

type Client struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		APIKey:     apiKey,
		BaseURL:    DefaultBaseURL,
		HTTPClient: &http.Client{},
	}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return DefaultBaseURL
}

// Stream posts to /v1/messages with stream:true, calls onText for each text_delta,
// and returns the concatenated assistant text.
func (c *Client) Stream(ctx context.Context, req Request, onText func(string) error) (string, error) {
	if c.APIKey == "" {
		return "", fmt.Errorf("anthropic: missing API key")
	}
	req.Stream = true
	if req.MaxTokens <= 0 {
		req.MaxTokens = 4096
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("x-api-key", c.APIKey)
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("anthropic-version", APIVersion)
	httpReq.Header.Set("accept", "text/event-stream")

	resp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", readAPIError(resp)
	}

	return readSSE(resp.Body, onText)
}

func readAPIError(resp *http.Response) error {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    "failed to read error body: " + err.Error(),
		}
	}
	return parseErrorBody(raw, resp.StatusCode)
}

// apiErrorObject is a pointer so JSON null / omitted nested error never panics.
type apiErrorObject struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type apiErrorPayload struct {
	Type  string          `json:"type"`
	Error *apiErrorObject `json:"error"`
}

// parseErrorBody builds an APIError from an HTTP or SSE error payload.
// statusCode may be 0 for stream-side errors (no HTTP status available).
func parseErrorBody(raw []byte, statusCode int) *APIError {
	trimmed := bytes.TrimSpace(raw)
	fallback := statusFallbackMessage(statusCode)

	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return &APIError{StatusCode: statusCode, Message: fallback}
	}

	var parsed apiErrorPayload
	if err := json.Unmarshal(trimmed, &parsed); err == nil {
		if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
			return &APIError{
				StatusCode: statusCode,
				Type:       parsed.Error.Type,
				Message:    parsed.Error.Message,
			}
		}
		if parsed.Type == "error" || parsed.Error != nil {
			typ := ""
			if parsed.Error != nil {
				typ = parsed.Error.Type
			}
			return &APIError{StatusCode: statusCode, Type: typ, Message: fallback}
		}
		// Valid JSON that is not an Anthropic error object (e.g. {}).
		return &APIError{StatusCode: statusCode, Message: fallback}
	}

	return &APIError{
		StatusCode: statusCode,
		Message:    sanitizeErrorMessage(string(trimmed)),
	}
}

func statusFallbackMessage(statusCode int) string {
	if statusCode != 0 {
		if text := http.StatusText(statusCode); text != "" {
			return text
		}
		return fmt.Sprintf("HTTP %d", statusCode)
	}
	return "stream error"
}

func sanitizeErrorMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	// Avoid echoing secrets if a proxy or misconfigured server reflects them.
	msg = strings.ReplaceAll(msg, "sk-ant-", "[redacted]")
	const maxLen = 512
	if len(msg) > maxLen {
		return msg[:maxLen] + "..."
	}
	return msg
}

type sseEvent struct {
	event string
	data  string
}

func readSSE(r io.Reader, onText func(string) error) (string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		cur     sseEvent
		full    strings.Builder
		stopped bool
	)

	flush := func() error {
		if cur.event == "" && cur.data == "" {
			return nil
		}
		err := handleSSEEvent(cur, &full, onText)
		cur = sseEvent{}
		return err
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				if errors.Is(err, errStreamDone) {
					stopped = true
					break
				}
				return full.String(), err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			cur.event = value
		case "data":
			if cur.data != "" {
				cur.data += "\n"
			}
			cur.data += value
		}
	}
	if !stopped {
		if err := flush(); err != nil {
			if errors.Is(err, errStreamDone) {
				stopped = true
			} else {
				return full.String(), err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return full.String(), err
	}
	if !stopped {
		return full.String(), ErrIncompleteStream
	}
	return full.String(), nil
}

var errStreamDone = errors.New("stream done")

func handleSSEEvent(ev sseEvent, full *strings.Builder, onText func(string) error) error {
	event := ev.event
	if event == "" {
		event = "message"
	}

	switch event {
	case "ping":
		return nil
	case "message_stop":
		return errStreamDone
	case "error":
		return parseStreamError(ev.data)
	}

	if ev.data == "" {
		return nil
	}

	var payload struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
		Error *apiErrorObject `json:"error"`
	}
	if err := json.Unmarshal([]byte(ev.data), &payload); err != nil {
		return fmt.Errorf("anthropic: invalid SSE data: %w", err)
	}

	if payload.Type == "error" || (payload.Error != nil && strings.TrimSpace(payload.Error.Message) != "") {
		return parseErrorBody([]byte(ev.data), 0)
	}

	if payload.Type == "content_block_delta" && payload.Delta.Type == "text_delta" && payload.Delta.Text != "" {
		full.WriteString(payload.Delta.Text)
		if onText != nil {
			if err := onText(payload.Delta.Text); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseStreamError(data string) error {
	return parseErrorBody([]byte(data), 0)
}
