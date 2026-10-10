package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"terminalchat/anthropic"
)

func main() {
	if err := loadDotEnv(".env"); err != nil {
		fmt.Fprintf(os.Stderr, "dotenv: %v\n", err)
		os.Exit(1)
	}

	defaultModel := os.Getenv("ANTHROPIC_MODEL")
	if defaultModel == "" {
		defaultModel = "claude-sonnet-4-5"
	}

	model := flag.String("model", defaultModel, "Anthropic model id")
	maxTokens := flag.Int("max-tokens", 4096, "max output tokens")
	system := flag.String("system", "", "optional system prompt")
	headerTimeout := flag.Duration("header-timeout", anthropic.DefaultResponseHeaderTimeout,
		"max time to wait for response headers (not the streamed body)")
	flag.Parse()

	if *headerTimeout <= 0 {
		fmt.Fprintln(os.Stderr, "-header-timeout must be positive")
		os.Exit(1)
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "ANTHROPIC_API_KEY is not set")
		os.Exit(1)
	}

	httpClient := anthropic.NewHTTPClient(anthropic.TransportConfig{
		ResponseHeaderTimeout: *headerTimeout,
	})
	client := anthropic.NewClientWithHTTPClient(apiKey, httpClient)
	hub := newInterruptHub()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	go func() {
		for range sigCh {
			if hub.HandleSignal() {
				// Idle interrupt: wake the prompt select so we can exit cleanly.
				// HandleSignal already signaled hub.Wake().
			}
		}
	}()

	fmt.Fprintf(os.Stderr, "model %s — type /quit to exit, /reset to clear history\n", *model)
	fmt.Fprintln(os.Stderr, "Ctrl+C cancels an in-flight reply; Ctrl+C at the prompt exits")

	lines := make(chan string)
	scanErr := make(chan error, 1)
	go func() {
		in := bufio.NewScanner(os.Stdin)
		in.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for in.Scan() {
			lines <- in.Text()
		}
		if err := in.Err(); err != nil {
			scanErr <- err
		}
		close(lines)
	}()

	out := bufio.NewWriter(os.Stdout)
	var history []anthropic.Message

	for {
		fmt.Fprint(os.Stdout, "you> ")
		_ = os.Stdout.Sync()

		var line string
		select {
		case <-hub.Wake():
			fmt.Fprintln(os.Stdout)
			return
		case err := <-scanErr:
			fmt.Fprintln(os.Stdout)
			fmt.Fprintf(os.Stderr, "stdin: %v\n", err)
			os.Exit(1)
		case text, ok := <-lines:
			if !ok {
				fmt.Fprintln(os.Stdout)
				return
			}
			if hub.ExitRequested() {
				fmt.Fprintln(os.Stdout)
				return
			}
			line = text
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch line {
		case "/quit", "/exit":
			return
		case "/reset":
			history = nil
			fmt.Fprintln(os.Stderr, "history cleared")
			continue
		}

		history = append(history, anthropic.Message{Role: "user", Content: line})
		req := anthropic.Request{
			Model:     *model,
			MaxTokens: *maxTokens,
			System:    *system,
			Messages:  history,
		}

		fmt.Fprint(os.Stdout, "claude> ")
		_ = out.Flush()

		reqCtx, endReq := hub.BeginRequest(context.Background())
		text, err := client.Stream(reqCtx, req, func(delta string) error {
			_, werr := out.WriteString(delta)
			if werr != nil {
				return werr
			}
			return out.Flush()
		})
		endReq()
		fmt.Fprintln(os.Stdout)

		if hub.ExitRequested() {
			return
		}

		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(reqCtx.Err(), context.Canceled) {
				fmt.Fprintln(os.Stderr, "interrupted — type another message, or Ctrl+C / /quit to exit")
				history = history[:len(history)-1]
				continue
			}
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			history = history[:len(history)-1]
			continue
		}
		if text != "" {
			history = append(history, anthropic.Message{Role: "assistant", Content: text})
		}
	}
}
