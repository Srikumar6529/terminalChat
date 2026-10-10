package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
