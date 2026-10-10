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

func TestLoadDotEnvEmptyEnvBlocksFile(t *testing.T) {
	// LookupEnv treats empty-but-set as present; .env must not override.
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("ANTHROPIC_API_KEY=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "")
	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "" {
		t.Fatalf("empty env should win over .env, got %q", got)
	}
}

func TestLoadDotEnvSkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "" +
		"not-a-assignment\n" +
		"=nokey\n" +
		"# comment\n" +
		"ANTHROPIC_API_KEY=ok\n" +
		"ANTHROPIC_MODEL\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	unsetEnvForTest(t, "ANTHROPIC_API_KEY")
	unsetEnvForTest(t, "ANTHROPIC_MODEL")
	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "ok" {
		t.Fatalf("ANTHROPIC_API_KEY = %q", got)
	}
	if _, ok := os.LookupEnv("ANTHROPIC_MODEL"); ok {
		t.Fatal("malformed ANTHROPIC_MODEL line should not set the variable")
	}
}
