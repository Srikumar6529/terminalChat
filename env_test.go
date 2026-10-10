package main

import (
	"os"
	"path/filepath"
	"testing"
)

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prev)
			return
		}
		_ = os.Unsetenv(key)
	})
}

func TestLoadDotEnvSetsMissingVars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# comment\nANTHROPIC_API_KEY=from-file\nexport ANTHROPIC_MODEL=\"claude-sonnet-4-5\"\nKEEP_ME=file\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	unsetEnvForTest(t, "ANTHROPIC_API_KEY")
	unsetEnvForTest(t, "ANTHROPIC_MODEL")
	t.Setenv("KEEP_ME", "shell")

	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "from-file" {
		t.Fatalf("ANTHROPIC_API_KEY = %q", got)
	}
	if got := os.Getenv("ANTHROPIC_MODEL"); got != "claude-sonnet-4-5" {
		t.Fatalf("ANTHROPIC_MODEL = %q", got)
	}
	if got := os.Getenv("KEEP_ME"); got != "shell" {
		t.Fatalf("KEEP_ME overwritten: %q", got)
	}
}

func TestLoadDotEnvMissingFile(t *testing.T) {
	if err := loadDotEnv(filepath.Join(t.TempDir(), "nope.env")); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDotEnvDoesNotOverrideExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("ANTHROPIC_API_KEY=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "from-shell")
	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "from-shell" {
		t.Fatalf("ANTHROPIC_API_KEY = %q", got)
	}
}
