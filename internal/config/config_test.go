package config

import "testing"

func TestLoadBuildsOrderedGeminiModels(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("DEV_AUTH_BYPASS", "true")
	t.Setenv("GEMINI_API_KEY", "test-key")
	t.Setenv("GEMINI_MODEL", "gemini-2.5-flash")
	t.Setenv("GEMINI_MODEL_2", "  gemini-3-flash-preview  ")
	t.Setenv("GEMINI_MODEL_3", "gemini-2.5-flash")
	t.Setenv("GEMINI_RPM_LIMIT", "5")
	t.Setenv("GEMINI_RPD_LIMIT", "20")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := len(cfg.Gemini.Models), 2; got != want {
		t.Fatalf("expected %d Gemini models, got %d (%v)", want, got, cfg.Gemini.Models)
	}
	if cfg.Gemini.Models[0] != "gemini-2.5-flash" {
		t.Fatalf("expected primary model to stay first, got %q", cfg.Gemini.Models[0])
	}
	if cfg.Gemini.Models[1] != "gemini-3-flash-preview" {
		t.Fatalf("expected secondary model to be trimmed and included, got %q", cfg.Gemini.Models[1])
	}
	if cfg.Gemini.RPMLimit != 5 {
		t.Fatalf("expected RPMLimit=5, got %d", cfg.Gemini.RPMLimit)
	}
	if cfg.Gemini.RPDLimit != 20 {
		t.Fatalf("expected RPDLimit=20, got %d", cfg.Gemini.RPDLimit)
	}
}
