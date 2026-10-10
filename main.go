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

	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfg, err := loadConfig(fs, os.Args[1:], os.Getenv)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "usage error: %v\n", err)
		fmt.Fprintln(os.Stderr, "run with -h for flags; config: CLI > env/.env > defaults")
		os.Exit(2)
	}

	httpClient := anthropic.NewHTTPClient(anthropic.TransportConfig{
		ResponseHeaderTimeout: cfg.HeaderTimeout,
	})
	client := anthropic.NewClientWithHTTPClient(cfg.APIKey, httpClient)
	hub := newInterruptHub()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	go func() {
		for range sigCh {
			hub.HandleSignal()
		}
	}()

	fmt.Fprintf(os.Stderr, "model %s — type /help for commands\n", cfg.Model)
	fmt.Fprintln(os.Stderr, "Ctrl+C cancels an in-flight reply; Ctrl+C at the prompt exits")

	lines := make(chan string)
	scanErr := make(chan error, 1)
	go func() {
		in := bufio.NewScanner(os.Stdin)
		in.Buffer(make([]byte, 0, 64*1024), maxInputBytes)
		for in.Scan() {
			lines <- in.Text()
		}
		if err := in.Err(); err != nil {
			if errors.Is(err, bufio.ErrTooLong) {
				scanErr <- fmt.Errorf("input line exceeds %d bytes; shorten the message and restart", maxInputBytes)
				return
			}
			scanErr <- err
			return
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
		case "/help":
			printHelp()
			continue
		case "/reset":
			history = nil
			fmt.Fprintln(os.Stderr, "history cleared")
			continue
		}

		history = append(history, anthropic.Message{Role: "user", Content: line})
		req := anthropic.Request{
			Model:     cfg.Model,
			MaxTokens: cfg.MaxTokens,
			System:    cfg.System,
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
		_ = out.Flush()

		if hub.ExitRequested() {
			return
		}

		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(reqCtx.Err(), context.Canceled) {
				fmt.Fprintln(os.Stderr, "interrupted — reply not saved; type another message, or Ctrl+C / /quit to exit")
			} else {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				fmt.Fprintln(os.Stderr, "reply not saved; conversation rolled back to before your last message")
			}
			history = finishTurn(history, text, err)
			continue
		}
		history = finishTurn(history, text, nil)
	}
}
