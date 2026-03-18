package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

type SettingsService struct {
	settingsRepo SettingsRepository
}

func NewSettingsService(settingsRepo SettingsRepository) *SettingsService {
	return &SettingsService{settingsRepo: settingsRepo}
}

func (s *SettingsService) Get(ctx context.Context, userID uuid.UUID) (domain.UserSettings, error) {
	return s.settingsRepo.Get(ctx, userID)
}

func (s *SettingsService) Update(ctx context.Context, input domain.UserSettings) (domain.UserSettings, error) {
	if input.Timezone == "" {
		input.Timezone = domain.DefaultTimezone
	}
	switch input.CEFRLevel {
	case domain.CEFRB1, domain.CEFRB2, domain.CEFRC1, domain.CEFRC2:
	default:
		return domain.UserSettings{}, fmt.Errorf("%w: unsupported cefr level", domain.ErrValidation)
	}
	if input.DailyNewWordLimit < 0 || input.DailyNewWordLimit > 50 {
		return domain.UserSettings{}, fmt.Errorf("%w: invalid daily_new_word_limit", domain.ErrValidation)
	}
	switch input.PreferredMeaningLanguage {
	case domain.MeaningLanguageVietnamese, domain.MeaningLanguageEnglish:
	default:
		return domain.UserSettings{}, fmt.Errorf("%w: invalid preferred_meaning_language", domain.ErrValidation)
	}
	return s.settingsRepo.Upsert(ctx, input)
}
