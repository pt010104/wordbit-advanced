package domain

import "github.com/google/uuid"

const (
	DefaultTimezone          = "Asia/Ho_Chi_Minh"
	DefaultDailyNewWordLimit = 10
	DefaultGeminiProvider    = "google-gemini"
)

func DefaultUserSettings(userID uuid.UUID) UserSettings {
	return UserSettings{
		UserID:                   userID,
		CEFRLevel:                CEFRB1,
		DailyNewWordLimit:        DefaultDailyNewWordLimit,
		PreferredMeaningLanguage: MeaningLanguageVietnamese,
		Timezone:                 DefaultTimezone,
		PronunciationEnabled:     true,
		LockScreenEnabled:        false,
	}
}
