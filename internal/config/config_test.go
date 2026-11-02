package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("interval: 2h\nwecom:\n  bot_id: from-file\n  secret: file-secret\n  admins: [alice]\n")
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG", path)
	t.Setenv("WECOM_BOT_ID", "from-env")
	t.Setenv("WECOM_SECRET", "env-secret")
	t.Setenv("WECOM_ADMINS", "bob, carol")
	t.Setenv("CHECK_INTERVAL", "10s")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BotID != "from-env" || cfg.Secret != "env-secret" {
		t.Fatalf("%+v", cfg)
	}
	if cfg.Interval != time.Minute {
		t.Fatalf("interval %s", cfg.Interval)
	}
	if len(cfg.Admins) != 2 || cfg.Admins[1] != "carol" {
		t.Fatalf("%#v", cfg.Admins)
	}
}

func TestDotEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	body := []byte("WECOM_BOT_ID=from-dotenv\nWECOM_SECRET=\"dotenv-secret\"\n# comment\nCHECK_INTERVAL=45m\n")
	if err := os.WriteFile(".env", body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG", "")
	t.Setenv("WECOM_BOT_ID", "")
	t.Setenv("WECOM_SECRET", "")
	t.Setenv("CHECK_INTERVAL", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BotID != "from-dotenv" || cfg.Secret != "dotenv-secret" {
		t.Fatalf("%+v", cfg)
	}
	if cfg.Interval != 45*time.Minute {
		t.Fatalf("interval %s", cfg.Interval)
	}
}

func TestEnvWinsOverDotEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	body := []byte("WECOM_BOT_ID=from-dotenv\nWECOM_SECRET=dotenv-secret\n")
	if err := os.WriteFile(".env", body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG", "")
	t.Setenv("WECOM_BOT_ID", "from-env")
	t.Setenv("WECOM_SECRET", "env-secret")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BotID != "from-env" || cfg.Secret != "env-secret" {
		t.Fatalf("%+v", cfg)
	}
}

func TestMissingCredentials(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("CONFIG", "")
	t.Setenv("WECOM_BOT_ID", "")
	t.Setenv("WECOM_SECRET", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "WECOM_BOT_ID") {
		t.Fatal(err)
	}
}
