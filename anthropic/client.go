package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

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
	if e.Type != "" {
		return fmt.Sprintf("anthropic: %s (%s, HTTP %d)", e.Message, e.Type, e.StatusCode)
	}
	return fmt.Sprintf("anthropic: %s (HTTP %d)", e.Message, e.StatusCode)
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
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &parsed) == nil && parsed.Error.Message != "" {
		return &APIError{StatusCode: resp.StatusCode, Type: parsed.Error.Type, Message: parsed.Error.Message}
	}
	msg := strings.TrimSpace(string(raw))
	if msg == "" {
		msg = resp.Status
	}
	return &APIError{StatusCode: resp.StatusCode, Message: msg}
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
				if err == errStreamDone {
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
		if err := flush(); err != nil && err != errStreamDone {
			return full.String(), err
		}
	}
	if err := scanner.Err(); err != nil {
		return full.String(), err
	}
	return full.String(), nil
}

var errStreamDone = fmt.Errorf("stream done")

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
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(ev.data), &payload); err != nil {
		return fmt.Errorf("anthropic: invalid SSE data: %w", err)
	}

	if payload.Type == "error" || payload.Error.Message != "" {
		msg := payload.Error.Message
		if msg == "" {
			msg = "stream error"
		}
		return &APIError{Type: payload.Error.Type, Message: msg}
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
	var payload struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(data), &payload) == nil && payload.Error.Message != "" {
		return &APIError{Type: payload.Error.Type, Message: payload.Error.Message}
	}
	if strings.TrimSpace(data) == "" {
		return &APIError{Message: "stream error"}
	}
	return &APIError{Message: data}
}
