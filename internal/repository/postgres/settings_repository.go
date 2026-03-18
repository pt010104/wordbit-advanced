package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"wordbit-advanced-app/backend/internal/domain"
)

type SettingsRepository struct {
	pool *pgxpool.Pool
}

func (r *SettingsRepository) Get(ctx context.Context, userID uuid.UUID) (domain.UserSettings, error) {
	return scanSettings(r.pool.QueryRow(ctx, `
		SELECT user_id, cefr_level, daily_new_word_limit, preferred_meaning_language, timezone, pronunciation_enabled, lock_screen_enabled, created_at, updated_at
		FROM user_settings
		WHERE user_id = $1
	`, userID))
}

func (r *SettingsRepository) Upsert(ctx context.Context, settings domain.UserSettings) (domain.UserSettings, error) {
	query := `
		INSERT INTO user_settings (
			user_id, cefr_level, daily_new_word_limit, preferred_meaning_language, timezone, pronunciation_enabled, lock_screen_enabled
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id) DO UPDATE SET
			cefr_level = EXCLUDED.cefr_level,
			daily_new_word_limit = EXCLUDED.daily_new_word_limit,
			preferred_meaning_language = EXCLUDED.preferred_meaning_language,
			timezone = EXCLUDED.timezone,
			pronunciation_enabled = EXCLUDED.pronunciation_enabled,
			lock_screen_enabled = EXCLUDED.lock_screen_enabled
		RETURNING user_id, cefr_level, daily_new_word_limit, preferred_meaning_language, timezone, pronunciation_enabled, lock_screen_enabled, created_at, updated_at
	`
	return scanSettings(r.pool.QueryRow(ctx, query,
		settings.UserID,
		settings.CEFRLevel,
		settings.DailyNewWordLimit,
		settings.PreferredMeaningLanguage,
		settings.Timezone,
		settings.PronunciationEnabled,
		settings.LockScreenEnabled,
	))
}
