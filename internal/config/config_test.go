package config

import "testing"

func TestLoadBuildsOrderedDeepSeekModels(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("DEV_AUTH_BYPASS", "true")
	t.Setenv("DS_KEY", "test-key")
	t.Setenv("DS_MODEL", "deepseek-v4-flash")
	t.Setenv("DS_MODEL_2", "  deepseek-v4-pro  ")
	t.Setenv("DS_MODEL_3", "deepseek-v4-flash")
	t.Setenv("DS_RPM_LIMIT", "5")
	t.Setenv("DS_RPD_LIMIT", "20")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := len(cfg.DeepSeek.Models), 2; got != want {
		t.Fatalf("expected %d DeepSeek models, got %d (%v)", want, got, cfg.DeepSeek.Models)
	}
	if cfg.DeepSeek.Models[0] != "deepseek-v4-flash" {
		t.Fatalf("expected primary model to stay first, got %q", cfg.DeepSeek.Models[0])
	}
	if cfg.DeepSeek.Models[1] != "deepseek-v4-pro" {
		t.Fatalf("expected secondary model to be trimmed and included, got %q", cfg.DeepSeek.Models[1])
	}
	if cfg.DeepSeek.RPMLimit != 5 {
		t.Fatalf("expected RPMLimit=5, got %d", cfg.DeepSeek.RPMLimit)
	}
	if cfg.DeepSeek.RPDLimit != 20 {
		t.Fatalf("expected RPDLimit=20, got %d", cfg.DeepSeek.RPDLimit)
	}
}
