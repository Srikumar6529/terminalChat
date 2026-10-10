package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"terminalchat/anthropic"
)

const (
	defaultModel     = "claude-sonnet-4-5"
	defaultMaxTokens = 4096
	maxMaxTokens     = 200_000
	maxInputBytes    = 1024 * 1024
)

// config holds validated runtime settings.
//
// Precedence (highest wins):
//  1. Explicit CLI flags (-model, -max-tokens, -system, -header-timeout)
//  2. Process environment (including values loaded from optional .env)
//  3. Built-in defaults
//
// ANTHROPIC_API_KEY is env/.env only (no flag) so keys are less likely to land
// in shell history. ANTHROPIC_MODEL sets the default for -model only; an
// explicit -model always wins after flag.Parse.
type config struct {
	APIKey        string
	Model         string
	MaxTokens     int
	System        string
	HeaderTimeout time.Duration
}

func loadConfig(fs *flag.FlagSet, args []string, getenv func(string) string) (config, error) {
	if fs == nil {
		fs = flag.NewFlagSet("terminalchat", flag.ContinueOnError)
	}
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	modelDefault := strings.TrimSpace(getenv("ANTHROPIC_MODEL"))
	if modelDefault == "" {
		modelDefault = defaultModel
	}

	model := fs.String("model", modelDefault, "Anthropic model id (default from ANTHROPIC_MODEL or "+defaultModel+")")
	maxTokens := fs.Int("max-tokens", defaultMaxTokens, "max output tokens (1..200000)")
	system := fs.String("system", "", "optional system prompt")
	headerTimeout := fs.Duration("header-timeout", anthropic.DefaultResponseHeaderTimeout,
		"max time to wait for response headers (not the streamed body)")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}

	cfg := config{
		APIKey:        strings.TrimSpace(getenv("ANTHROPIC_API_KEY")),
		Model:         strings.TrimSpace(*model),
		MaxTokens:     *maxTokens,
		System:        *system,
		HeaderTimeout: *headerTimeout,
	}
	if err := cfg.validate(); err != nil {
		return config{}, err
	}
	return cfg, nil
}

func (c config) validate() error {
	if c.APIKey == "" {
		return errors.New("ANTHROPIC_API_KEY is not set (export it or add it to .env; see .env.example)")
	}
	if c.Model == "" {
		return errors.New("-model must not be empty (set -model or ANTHROPIC_MODEL)")
	}
	if c.MaxTokens < 1 || c.MaxTokens > maxMaxTokens {
		return fmt.Errorf("-max-tokens must be between 1 and %d (got %d)", maxMaxTokens, c.MaxTokens)
	}
	if c.HeaderTimeout <= 0 {
		return errors.New("-header-timeout must be positive")
	}
	return nil
}

func printHelp() {
	fmt.Fprintln(os.Stderr, `commands:
  /help          show this help
  /reset         clear conversation history
  /quit, /exit   leave the chat

Ctrl+C cancels an in-flight reply; Ctrl+C at the prompt exits.

configuration precedence:
  CLI flags > environment (and .env for unset keys) > built-in defaults
  ANTHROPIC_API_KEY is required (env/.env only)
  ANTHROPIC_MODEL sets the default for -model`)
}
