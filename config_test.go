package main

import (
	"bytes"
	"flag"
	"strings"
	"testing"
	"time"

	"terminalchat/anthropic"
)

func TestLoadConfigDefaultsAndEnv(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(&bytes.Buffer{})
	getenv := func(k string) string {
		switch k {
		case "ANTHROPIC_API_KEY":
			return "sk-test"
		case "ANTHROPIC_MODEL":
			return "env-model"
		default:
			return ""
		}
	}
	cfg, err := loadConfig(fs, nil, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "env-model" {
		t.Fatalf("Model = %q", cfg.Model)
	}
	if cfg.MaxTokens != defaultMaxTokens {
		t.Fatalf("MaxTokens = %d", cfg.MaxTokens)
	}
	if cfg.HeaderTimeout != anthropic.DefaultResponseHeaderTimeout {
		t.Fatalf("HeaderTimeout = %v", cfg.HeaderTimeout)
	}
}

func TestLoadConfigFlagOverridesEnvModel(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(&bytes.Buffer{})
	getenv := func(k string) string {
		if k == "ANTHROPIC_API_KEY" {
			return "sk-test"
		}
		if k == "ANTHROPIC_MODEL" {
			return "env-model"
		}
		return ""
	}
	cfg, err := loadConfig(fs, []string{"-model", "flag-model"}, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "flag-model" {
		t.Fatalf("Model = %q, want flag-model", cfg.Model)
	}
}

func TestLoadConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want string
	}{
		{
			name: "missing key",
			env:  map[string]string{},
			want: "ANTHROPIC_API_KEY",
		},
		{
			name: "bad max tokens zero",
			args: []string{"-max-tokens", "0"},
			env:  map[string]string{"ANTHROPIC_API_KEY": "k"},
			want: "-max-tokens",
		},
		{
			name: "bad max tokens high",
			args: []string{"-max-tokens", "200001"},
			env:  map[string]string{"ANTHROPIC_API_KEY": "k"},
			want: "-max-tokens",
		},
		{
			name: "bad header timeout",
			args: []string{"-header-timeout", "0s"},
			env:  map[string]string{"ANTHROPIC_API_KEY": "k"},
			want: "-header-timeout",
		},
		{
			name: "empty model flag",
			args: []string{"-model", "  "},
			env:  map[string]string{"ANTHROPIC_API_KEY": "k"},
			want: "-model",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			fs.SetOutput(&bytes.Buffer{})
			getenv := func(k string) string { return tt.env[k] }
			_, err := loadConfig(fs, tt.args, getenv)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestLoadConfigAcceptsCustomHeaderTimeout(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(&bytes.Buffer{})
	getenv := func(k string) string {
		if k == "ANTHROPIC_API_KEY" {
			return "k"
		}
		return ""
	}
	cfg, err := loadConfig(fs, []string{"-header-timeout", "15s", "-max-tokens", "100"}, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HeaderTimeout != 15*time.Second || cfg.MaxTokens != 100 {
		t.Fatalf("cfg = %+v", cfg)
	}
}
