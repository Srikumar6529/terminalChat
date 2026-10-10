package anthropic

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStreamAccumulatesTextDeltas(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("x-api-key = %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != APIVersion {
			t.Errorf("anthropic-version = %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		var req Request
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Fatal(err)
		}
		if !req.Stream {
			t.Error("expected stream:true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\"}\n\n")
		io.WriteString(w, "event: ping\ndata: {\"type\":\"ping\"}\n\n")
		io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n")
		io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n")
		io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer ts.Close()

	c := NewClient("test-key")
	c.BaseURL = ts.URL
	c.HTTPClient = ts.Client()

	var got []string
	text, err := c.Stream(context.Background(), Request{
		Model:     "claude-sonnet-4-5",
		MaxTokens: 16,
		Messages:  []Message{{Role: "user", Content: "hi"}},
	}, func(s string) error {
		got = append(got, s)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "Hello world" {
		t.Fatalf("text = %q", text)
	}
	if strings.Join(got, "") != "Hello world" {
		t.Fatalf("deltas = %q", got)
	}
}

func TestStreamHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
	}))
	defer ts.Close()

	c := NewClient("bad")
	c.BaseURL = ts.URL
	c.HTTPClient = ts.Client()

	_, err := c.Stream(context.Background(), Request{Model: "x", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type %T: %v", err, err)
	}
	if apiErr.StatusCode != 401 || apiErr.Type != "authentication_error" {
		t.Fatalf("unexpected error: %+v", apiErr)
	}
}

func TestStreamEventError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"try again\"}}\n\n")
	}))
	defer ts.Close()

	c := NewClient("test-key")
	c.BaseURL = ts.URL
	c.HTTPClient = ts.Client()

	_, err := c.Stream(context.Background(), Request{Model: "x", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type %T: %v", err, err)
	}
	if apiErr.Type != "overloaded_error" || apiErr.Message != "try again" {
		t.Fatalf("unexpected error: %+v", apiErr)
	}
}

func TestReadSSEEOFBeforeMessageStop(t *testing.T) {
	text, err := readSSE(strings.NewReader(""), nil)
	if !errors.Is(err, ErrIncompleteStream) {
		t.Fatalf("err = %v, want ErrIncompleteStream", err)
	}
	if text != "" {
		t.Fatalf("text = %q", text)
	}
}

func TestReadSSEEOFAfterTextDelta(t *testing.T) {
	body := "" +
		"event: message_start\ndata: {\"type\":\"message_start\"}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n"
	var deltas []string
	text, err := readSSE(strings.NewReader(body), func(s string) error {
		deltas = append(deltas, s)
		return nil
	})
	if !errors.Is(err, ErrIncompleteStream) {
		t.Fatalf("err = %v, want ErrIncompleteStream", err)
	}
	if text != "partial" {
		t.Fatalf("text = %q", text)
	}
	if strings.Join(deltas, "") != "partial" {
		t.Fatalf("deltas = %q", deltas)
	}
}

func TestReadSSEMessageStopThenEOF(t *testing.T) {
	body := "" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	text, err := readSSE(strings.NewReader(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != "ok" {
		t.Fatalf("text = %q", text)
	}
}

func TestReadSSEFinalEventWithoutTrailingBlankLine(t *testing.T) {
	// message_stop is the last event and is not followed by a blank line.
	body := "" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"done\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}"
	text, err := readSSE(strings.NewReader(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != "done" {
		t.Fatalf("text = %q", text)
	}
}

func TestReadSSEMalformedEventData(t *testing.T) {
	body := "event: content_block_delta\ndata: {not-json\n\n"
	_, err := readSSE(strings.NewReader(body), nil)
	if err == nil {
		t.Fatal("expected error for malformed SSE data")
	}
	if errors.Is(err, ErrIncompleteStream) {
		t.Fatalf("got ErrIncompleteStream, want JSON parse error: %v", err)
	}
}

func TestReadSSECallbackError(t *testing.T) {
	body := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"x\"}}\n\n"
	want := errors.New("callback failed")
	_, err := readSSE(strings.NewReader(body), func(string) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestReadSSEScannerTooLong(t *testing.T) {
	// One SSE line longer than the 1MiB scanner limit.
	long := strings.Repeat("a", 1024*1024+1)
	body := "data: " + long + "\n\n"
	_, err := readSSE(strings.NewReader(body), nil)
	if err == nil {
		t.Fatal("expected scanner error")
	}
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("err = %v, want bufio.ErrTooLong", err)
	}
}

func TestParseErrorBodyCases(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		status     int
		wantType   string
		wantMsg    string
		wantStatus int
	}{
		{name: "empty", raw: "", status: 500, wantMsg: "Internal Server Error", wantStatus: 500},
		{name: "json null", raw: "null", status: 502, wantMsg: "Bad Gateway", wantStatus: 502},
		{name: "empty object", raw: "{}", status: 400, wantMsg: "Bad Request", wantStatus: 400},
		{name: "error null nested", raw: `{"type":"error","error":null}`, status: 401, wantMsg: "Unauthorized", wantStatus: 401},
		{name: "error missing message", raw: `{"type":"error","error":{"type":"api_error"}}`, status: 500, wantType: "api_error", wantMsg: "Internal Server Error", wantStatus: 500},
		{name: "full error", raw: `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`, status: 401, wantType: "authentication_error", wantMsg: "invalid x-api-key", wantStatus: 401},
		{name: "invalid json", raw: "not-json", status: 503, wantMsg: "not-json", wantStatus: 503},
		{name: "plain text", raw: " upstream blew up ", status: 502, wantMsg: "upstream blew up", wantStatus: 502},
		{name: "stream empty", raw: "", status: 0, wantMsg: "stream error", wantStatus: 0},
		{name: "stream full", raw: `{"type":"error","error":{"type":"overloaded_error","message":"try again"}}`, status: 0, wantType: "overloaded_error", wantMsg: "try again", wantStatus: 0},
		{name: "redacts api key", raw: "bad sk-ant-secret-value-12345678 here", status: 400, wantMsg: "bad [redacted] here", wantStatus: 400},
		{name: "redacts x-api-key", raw: "x-api-key: super-secret-token", status: 400, wantMsg: "[redacted]", wantStatus: 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseErrorBody([]byte(tt.raw), tt.status)
			if got.StatusCode != tt.wantStatus {
				t.Fatalf("StatusCode = %d, want %d", got.StatusCode, tt.wantStatus)
			}
			if got.Type != tt.wantType {
				t.Fatalf("Type = %q, want %q", got.Type, tt.wantType)
			}
			if got.Message != tt.wantMsg {
				t.Fatalf("Message = %q, want %q", got.Message, tt.wantMsg)
			}
			// Error() must not panic and must not claim HTTP 0 for stream errors.
			_ = got.Error()
			if tt.wantStatus == 0 && strings.Contains(got.Error(), "HTTP 0") {
				t.Fatalf("Error() = %q includes HTTP 0", got.Error())
			}
		})
	}
}

func TestReadAPIErrorBodyReadFailure(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(errReader{}),
	}
	err := readAPIError(resp)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type %T: %v", err, err)
	}
	if apiErr.StatusCode != 502 {
		t.Fatalf("StatusCode = %d", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Message, "failed to read error body") {
		t.Fatalf("Message = %q", apiErr.Message)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestParseStreamErrorConsistentWithHTTP(t *testing.T) {
	raw := `{"type":"error","error":{"type":"overloaded_error","message":"try again"}}`
	streamErr := parseStreamError(raw).(*APIError)
	httpErr := parseErrorBody([]byte(raw), 529)
	if streamErr.Type != httpErr.Type || streamErr.Message != httpErr.Message {
		t.Fatalf("stream=%+v http=%+v", streamErr, httpErr)
	}
	if streamErr.StatusCode != 0 || httpErr.StatusCode != 529 {
		t.Fatalf("status stream=%d http=%d", streamErr.StatusCode, httpErr.StatusCode)
	}
}

func TestMessagesURL(t *testing.T) {
	c := NewClient("k")
	c.BaseURL = "https://example.com/"
	got, err := c.messagesURL()
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.com/v1/messages" {
		t.Fatalf("url = %q", got)
	}
	c.BaseURL = "not a url"
	if _, err := c.messagesURL(); err == nil {
		t.Fatal("expected invalid base URL error")
	}
}

func TestStreamBaseURLTrailingSlash(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer ts.Close()

	c := NewClient("test-key")
	c.BaseURL = ts.URL + "/"
	c.HTTPClient = ts.Client()
	if _, err := c.Stream(context.Background(), Request{Model: "x", Messages: []Message{{Role: "user", Content: "hi"}}}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestReadSSECommentsAndUnknownFields(t *testing.T) {
	body := "" +
		": keep-alive\n" +
		"event: content_block_delta\n" +
		"id: evt_1\n" +
		"retry: 1000\n" +
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"},\"extra\":123}\n" +
		"\n" +
		"event: future_event\n" +
		"data: {\"type\":\"future_type\",\"foo\":true}\n" +
		"\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n" +
		"\n"
	text, err := readSSE(strings.NewReader(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != "hi" {
		t.Fatalf("text = %q", text)
	}
}

func TestReadSSEMultilineData(t *testing.T) {
	body := "" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\n" +
		"data: \"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n" +
		"\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n" +
		"\n"
	text, err := readSSE(strings.NewReader(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Hi" {
		t.Fatalf("text = %q", text)
	}
}

func TestReadSSEEmptyTextDeltaIgnored(t *testing.T) {
	calls := 0
	body := "" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"x\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	text, err := readSSE(strings.NewReader(body), func(string) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "x" || calls != 1 {
		t.Fatalf("text=%q calls=%d", text, calls)
	}
}

func TestReadSSEIgnoresNonTextDeltaTypes(t *testing.T) {
	body := "" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"t\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	text, err := readSSE(strings.NewReader(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != "t" {
		t.Fatalf("text = %q", text)
	}
}

func TestReadSSEKnownEventsSkipJSON(t *testing.T) {
	// message_start with broken JSON must not fail the stream.
	body := "" +
		"event: message_start\ndata: {not-json\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"a\"}}\n\n" +
		"event: message_delta\ndata: {also-bad\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	text, err := readSSE(strings.NewReader(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != "a" {
		t.Fatalf("text = %q", text)
	}
}

func TestReadSSEDataTypeMessageStopWithoutEventField(t *testing.T) {
	body := "data: {\"type\":\"message_stop\"}\n\n"
	if _, err := readSSE(strings.NewReader(body), nil); err != nil {
		t.Fatal(err)
	}
}

func TestReadSSEErrorAfterPartialText(t *testing.T) {
	body := "" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n" +
		"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"api_error\",\"message\":\"boom\"}}\n\n"
	var deltas []string
	text, err := readSSE(strings.NewReader(body), func(s string) error {
		deltas = append(deltas, s)
		return nil
	})
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type %T: %v", err, err)
	}
	if apiErr.Message != "boom" {
		t.Fatalf("err = %+v", apiErr)
	}
	if text != "partial" || strings.Join(deltas, "") != "partial" {
		t.Fatalf("text=%q deltas=%q", text, deltas)
	}
}

func TestReadSSECRLF(t *testing.T) {
	body := "event: content_block_delta\r\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"crlf\"}}\r\n\r\nevent: message_stop\r\ndata: {\"type\":\"message_stop\"}\r\n\r\n"
	text, err := readSSE(strings.NewReader(body), nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != "crlf" {
		t.Fatalf("text = %q", text)
	}
}

func TestStreamCanceledIncompleteIsContextCanceled(t *testing.T) {
	started := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		close(started)
		// Hang until the client cancels and the connection drops.
		<-r.Context().Done()
	}))
	defer ts.Close()

	c := NewClient("test-key")
	c.BaseURL = ts.URL
	c.HTTPClient = ts.Client()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	var text string
	go func() {
		var err error
		text, err = c.Stream(ctx, Request{Model: "x", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
		errCh <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not start streaming")
	}
	cancel()

	err := <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if text != "partial" && text != "" {
		// Partial text may or may not have been delivered before cancel.
		t.Logf("text = %q", text)
	}
}

func TestStreamRejectsJSONContentType(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"ok":true}`)
	}))
	defer ts.Close()

	c := NewClient("test-key")
	c.BaseURL = ts.URL
	c.HTTPClient = ts.Client()
	_, err := c.Stream(context.Background(), Request{Model: "x", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "Content-Type") {
		t.Fatalf("err = %v", err)
	}
}
