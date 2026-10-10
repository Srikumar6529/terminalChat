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
	"net/url"
	"regexp"
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

// NewClient returns a Client that reuses one HTTP client with connection-level
// and response-header timeouts (see NewHTTPClient). It does not set an overall
// http.Client.Timeout, so streamed bodies may run until the request context ends.
func NewClient(apiKey string) *Client {
	return NewClientWithHTTPClient(apiKey, NewHTTPClient(TransportConfig{}))
}

// NewClientWithHTTPClient is like NewClient but uses the provided HTTP client.
// The same client instance is reused for every Stream call.
func NewClientWithHTTPClient(apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = NewHTTPClient(TransportConfig{})
	}
	return &Client{
		APIKey:     apiKey,
		BaseURL:    DefaultBaseURL,
		HTTPClient: httpClient,
	}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	// Lazily install a shared client so repeated Stream calls reuse connections.
	c.HTTPClient = NewHTTPClient(TransportConfig{})
	return c.HTTPClient
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return DefaultBaseURL
}

// messagesURL builds POST /v1/messages against BaseURL (origin only, no /v1 suffix).
func (c *Client) messagesURL() (string, error) {
	raw := c.baseURL()
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("anthropic: invalid base URL %q: %w", raw, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("anthropic: invalid base URL %q: missing scheme or host", raw)
	}
	return u.JoinPath("v1", "messages").String(), nil
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

	endpoint, err := c.messagesURL()
	if err != nil {
		return "", err
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("x-api-key", c.APIKey)
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("anthropic-version", APIVersion)
	httpReq.Header.Set("accept", "text/event-stream")

	resp, err := c.httpClient().Do(httpReq)
	if err != nil {
		return "", annotateCtxErr(ctx, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", readAPIError(resp)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "" && strings.Contains(ct, "application/json") {
		return "", fmt.Errorf("anthropic: unexpected Content-Type %q for stream=true (want text/event-stream)", ct)
	}

	text, err := readSSE(resp.Body, onText)
	if err != nil {
		return text, annotateCtxErr(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return text, err
	}
	return text, nil
}

// annotateCtxErr ensures callers can use errors.Is(err, context.Canceled|DeadlineExceeded)
// when the request context ended, even if the transport/SSE layer returned a different error
// (commonly ErrIncompleteStream after a canceled body).
func annotateCtxErr(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(err, ctxErr) {
			return err
		}
		return fmt.Errorf("%w: %w", ctxErr, err)
	}
	return err
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

var (
	secretKeyPattern = regexp.MustCompile(`(?i)(sk-ant-[a-z0-9_\-]{8,}|x-api-key\s*[:=]\s*\S+)`)
)

func sanitizeErrorMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	// Avoid echoing secrets if a proxy or misconfigured server reflects them.
	msg = secretKeyPattern.ReplaceAllString(msg, "[redacted]")
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
			// SSE comment / keep-alive
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			// Per SSE, a line without ':' is a field name with empty value; ignore.
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			cur.event = value
		case "data":
			if cur.data != "" {
				cur.data += "\n"
			}
			cur.data += value
		case "id", "retry":
			// Recognized SSE fields; unused by this client.
		default:
			// Unknown fields are ignored for forward compatibility.
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

	// Documented Anthropic stream events that carry no assistant text for this client.
	// Ignore without requiring JSON so optional/unknown fields cannot break the stream.
	switch event {
	case "ping", "message_start", "content_block_start", "content_block_stop", "message_delta":
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
		Delta *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
		Error *apiErrorObject `json:"error"`
	}
	if err := json.Unmarshal([]byte(ev.data), &payload); err != nil {
		return fmt.Errorf("anthropic: invalid SSE data: %w", err)
	}

	// Prefer data.type when the SSE event: field is missing or unknown.
	switch payload.Type {
	case "message_stop":
		return errStreamDone
	case "ping", "message_start", "content_block_start", "content_block_stop", "message_delta":
		return nil
	case "error":
		return parseErrorBody([]byte(ev.data), 0)
	}

	if payload.Error != nil && strings.TrimSpace(payload.Error.Message) != "" {
		return parseErrorBody([]byte(ev.data), 0)
	}

	if payload.Type == "content_block_delta" {
		if payload.Delta == nil || payload.Delta.Type != "text_delta" {
			// e.g. input_json_delta / thinking_delta — ignore for text-only client
			return nil
		}
		if payload.Delta.Text == "" {
			return nil
		}
		full.WriteString(payload.Delta.Text)
		if onText != nil {
			if err := onText(payload.Delta.Text); err != nil {
				return err
			}
		}
		return nil
	}

	// Unknown event/type combinations are ignored for forward compatibility.
	return nil
}

func parseStreamError(data string) error {
	return parseErrorBody([]byte(data), 0)
}
