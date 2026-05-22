package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadAbsentFileUsesDefaults(t *testing.T) {
	t.Setenv("NATSIE_CONFIG", filepath.Join(t.TempDir(), "absent.yaml"))
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Defaults.MinIdle != 24*time.Hour {
		t.Errorf("MinIdle=%v want 24h", cfg.Defaults.MinIdle)
	}
	if cfg.Defaults.Format != "tsv" {
		t.Errorf("Format=%q want tsv", cfg.Defaults.Format)
	}
}

func TestLoadFileAndBotConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `
defaults:
  min_pending: 5000
  min_idle: 6h

contexts:
  snapp-js-main-teh1:
    peer: snapp-js-main-teh2

bot:
  schedules:
    - name: daily
      cron: "0 3 * * *"
      context: snapp-js-main-teh1
      peer_context: snapp-js-main-teh2
      min_pending: 10000
      min_idle: 24h
  notify:
    - stdout://
    - mattermost://chat.example.com/hooks/abc?channel=cleanup
  store: file:///var/lib/natsie/manifests
  audit_log: /var/lib/natsie/audit.jsonl
  http:
    listen: ":8080"
    base_url: https://natsie.example.com
  signing_key: secret
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Defaults.MinPending != 5000 {
		t.Errorf("Defaults.MinPending=%d want 5000", cfg.Defaults.MinPending)
	}
	if cfg.Contexts["snapp-js-main-teh1"].Peer != "snapp-js-main-teh2" {
		t.Errorf("peer mapping not loaded: %+v", cfg.Contexts)
	}
	if len(cfg.Bot.Schedules) != 1 || cfg.Bot.Schedules[0].Cron != "0 3 * * *" {
		t.Errorf("bot schedules not loaded: %+v", cfg.Bot.Schedules)
	}
	if cfg.Bot.Schedules[0].MinIdle != 24*time.Hour {
		t.Errorf("schedule MinIdle=%v want 24h", cfg.Bot.Schedules[0].MinIdle)
	}
	if len(cfg.Bot.Notify) != 2 {
		t.Errorf("notify=%v want 2 entries", cfg.Bot.Notify)
	}
	if cfg.Bot.HTTP.Listen != ":8080" {
		t.Errorf("HTTP.Listen=%q want :8080", cfg.Bot.HTTP.Listen)
	}
	if cfg.Bot.SigningKey != "secret" {
		t.Errorf("signing key not loaded")
	}
}

func TestEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("defaults:\n  min_pending: 1000\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Setenv("NATSIE_DEFAULTS__MIN_PENDING", "9999")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Defaults.MinPending != 9999 {
		t.Errorf("MinPending=%d want 9999 (env override failed)", cfg.Defaults.MinPending)
	}
}
