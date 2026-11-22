package config_test

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/platform/config"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInstanceKeyIsRequiredAndDecoded(t *testing.T) {
	key := strings.Repeat("a", 32)
	configuration, err := config.Load(writeConfig(t, "database_url = \"postgres://localhost/test\"\ninstance_key = \""+base64.StdEncoding.EncodeToString([]byte(key))+"\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := configuration.Key()
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != key {
		t.Fatalf("decoded key = %q", decoded)
	}
}

func TestInstanceKeyRejectsMissingAndInvalidValues(t *testing.T) {
	const database = "database_url = \"postgres://localhost/test\"\n"
	if _, err := config.Load(writeConfig(t, database)); err == nil {
		t.Fatal("a missing instance key must fail")
	}
	if _, err := config.Load(writeConfig(t, database+"instance_key = \"not-base64\"\n")); err == nil {
		t.Fatal("an invalid instance key must fail")
	}
	short := base64.StdEncoding.EncodeToString([]byte("short"))
	if _, err := config.Load(writeConfig(t, database+"instance_key = \""+short+"\"\n")); err == nil {
		t.Fatal("a non-32-byte instance key must fail")
	}
}

func TestInstanceKeyIsNotReadFromTheEnvironment(t *testing.T) {
	t.Setenv("SAFEAGENT_INSTANCE_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))))
	if _, err := config.Load(writeConfig(t, "database_url = \"postgres://localhost/test\"\n")); err == nil {
		t.Fatal("an environment variable must not satisfy the instance key")
	}
}
