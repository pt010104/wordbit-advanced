package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"wordbit-advanced-app/backend/internal/domain"
)

type Config struct {
	AppEnv                      string
	Port                        string
	LogLevel                    string
	DatabaseURL                 string
	DefaultTimezone             string
	AdminToken                  string
	AutoMigrate                 bool
	Mode4Enabled                bool
	MemoryCauseInferenceEnabled bool
	HTTPReadTimeout             time.Duration
	HTTPWriteTimeout            time.Duration
	HTTPIdleTimeout             time.Duration
	Gemini                      GeminiConfig
	Auth                        AuthConfig
	Scheduler                   SchedulerConfig
}

type GeminiConfig struct {
	BaseURL         string
	Models          []string
	APIKey          string
	Timeout         time.Duration
	MaxRetries      int
	Temperature     float64
	MaxOutputTokens int
	RPMLimit        int
	RPDLimit        int
}

type AuthConfig struct {
	DevBypass  bool
	DevSubject string
	DevEmail   string
	JWKSURL    string
	Issuer     string
	Audience   string
}

type SchedulerConfig struct {
	Enabled                  bool
	DailyPoolPrewarmCron     string
	DynamicReviewPrewarmCron string
	WeaknessRefreshCron      string
	ActiveUserLookback       time.Duration
}

func Load() (Config, error) {
	_ = godotenv.Load(".env")

	cfg := Config{
		AppEnv:                      envString("APP_ENV", "development"),
		Port:                        envString("PORT", "8080"),
		LogLevel:                    envString("LOG_LEVEL", "info"),
		DatabaseURL:                 envString("DATABASE_URL", ""),
		DefaultTimezone:             envString("DEFAULT_TIMEZONE", domain.DefaultTimezone),
		AdminToken:                  envString("ADMIN_TOKEN", ""),
		AutoMigrate:                 envBool("AUTO_MIGRATE", true),
		Mode4Enabled:                envBool("MODE4_ENABLED", true),
		MemoryCauseInferenceEnabled: envBool("MEMORY_CAUSE_INFERENCE_ENABLED", true),
		HTTPReadTimeout:             envDuration("HTTP_READ_TIMEOUT", 10*time.Second),
		HTTPWriteTimeout:            envDuration("HTTP_WRITE_TIMEOUT", 45*time.Second),
		HTTPIdleTimeout:             envDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
		Gemini: GeminiConfig{
			BaseURL:         envString("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com/v1beta"),
			Models:          buildGeminiModels(),
			APIKey:          resolveSecret("GEMINI_API_KEY", "GEMINI_API_KEY_FILE"),
			Timeout:         envDuration("GEMINI_TIMEOUT", 20*time.Second),
			MaxRetries:      envInt("GEMINI_MAX_RETRIES", 3),
			Temperature:     envFloat("GEMINI_TEMPERATURE", 0.4),
			MaxOutputTokens: envInt("GEMINI_MAX_OUTPUT_TOKENS", 4096),
			RPMLimit:        envInt("GEMINI_RPM_LIMIT", 0),
			RPDLimit:        envInt("GEMINI_RPD_LIMIT", 0),
		},
		Auth: AuthConfig{
			DevBypass:  envBool("DEV_AUTH_BYPASS", false),
			DevSubject: envString("DEV_AUTH_SUBJECT", "dev-user"),
			DevEmail:   envString("DEV_AUTH_EMAIL", "dev@example.com"),
			JWKSURL:    envString("AUTH_JWKS_URL", ""),
			Issuer:     envString("AUTH_ISSUER", ""),
			Audience:   envString("AUTH_AUDIENCE", ""),
		},
		Scheduler: SchedulerConfig{
			Enabled:                  envBool("CRON_ENABLED", true),
			DailyPoolPrewarmCron:     envString("CRON_DAILY_POOL_PREWARM_SCHEDULE", "* * * * *"),
			DynamicReviewPrewarmCron: envString("CRON_PREWARM_SCHEDULE", "10 0 * * *"),
			WeaknessRefreshCron:      envString("CRON_WEAKNESS_SCHEDULE", "0 * * * *"),
			ActiveUserLookback:       envDuration("ACTIVE_USER_LOOKBACK", 168*time.Hour),
		},
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if !cfg.Auth.DevBypass {
		if cfg.Auth.JWKSURL == "" || cfg.Auth.Issuer == "" || cfg.Auth.Audience == "" {
			return Config{}, errors.New("AUTH_JWKS_URL, AUTH_ISSUER, and AUTH_AUDIENCE are required unless DEV_AUTH_BYPASS=true")
		}
	}
	if cfg.Gemini.APIKey == "" {
		return Config{}, errors.New("GEMINI_API_KEY or GEMINI_API_KEY_FILE is required")
	}
	return cfg, nil
}

func buildGeminiModels() []string {
	candidates := []string{
		envString("GEMINI_MODEL", "gemini-2.0-flash"),
		strings.TrimSpace(os.Getenv("GEMINI_MODEL_2")),
		strings.TrimSpace(os.Getenv("GEMINI_MODEL_3")),
	}
	models := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, model := range candidates {
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}
	return models
}

func resolveSecret(envKey string, fileKey string) string {
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
		return value
	}
	filePath := strings.TrimSpace(os.Getenv(fileKey))
	if filePath == "" {
		return ""
	}
	bytes, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(bytes))
}

func envString(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envFloat(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func (c Config) Address() string {
	return fmt.Sprintf(":%s", c.Port)
}
