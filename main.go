package main

import (
	"bufio"
	"context"
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
	flag.Parse()

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "ANTHROPIC_API_KEY is not set")
		os.Exit(1)
	}

	client := anthropic.NewClient(apiKey)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Fprintf(os.Stderr, "model %s — type /quit to exit, /reset to clear history\n", *model)

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	out := bufio.NewWriter(os.Stdout)

	var history []anthropic.Message
	for {
		fmt.Fprint(os.Stdout, "you> ")
		if !in.Scan() {
			fmt.Fprintln(os.Stdout)
			break
		}
		line := strings.TrimSpace(in.Text())
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

		text, err := client.Stream(ctx, req, func(delta string) error {
			_, werr := out.WriteString(delta)
			if werr != nil {
				return werr
			}
			return out.Flush()
		})
		fmt.Fprintln(os.Stdout)
		if err != nil {
			if ctx.Err() != nil {
				fmt.Fprintln(os.Stderr, "interrupted")
				return
			}
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			history = history[:len(history)-1]
			continue
		}
		if text != "" {
			history = append(history, anthropic.Message{Role: "assistant", Content: text})
		}
	}
	if err := in.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "stdin: %v\n", err)
		os.Exit(1)
	}
}
