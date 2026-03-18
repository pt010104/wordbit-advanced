package apihttp

import (
	"errors"
	"fmt"
	nethttp "net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
	"wordbit-advanced-app/backend/internal/service"
)

func (h *Handler) GetSettings(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, err := currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	settings, err := h.settings.Get(r.Context(), user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, settings)
}

func (h *Handler) UpdateSettings(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, err := currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var payload struct {
		CEFRLevel                domain.CEFRLevel       `json:"cefr_level"`
		DailyNewWordLimit        int                    `json:"daily_new_word_limit"`
		PreferredMeaningLanguage domain.MeaningLanguage `json:"preferred_meaning_language"`
		Timezone                 string                 `json:"timezone"`
		PronunciationEnabled     bool                   `json:"pronunciation_enabled"`
		LockScreenEnabled        bool                   `json:"lock_screen_enabled"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, errors.New(domain.ErrValidation.Error()+": invalid json body"))
		return
	}
	settings, err := h.settings.Update(r.Context(), domain.UserSettings{
		UserID:                   user.ID,
		CEFRLevel:                payload.CEFRLevel,
		DailyNewWordLimit:        payload.DailyNewWordLimit,
		PreferredMeaningLanguage: payload.PreferredMeaningLanguage,
		Timezone:                 payload.Timezone,
		PronunciationEnabled:     payload.PronunciationEnabled,
		LockScreenEnabled:        payload.LockScreenEnabled,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, settings)
}

func (h *Handler) ListDictionaryWords(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, err := currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	limit, err := parseOptionalInt(r.URL.Query().Get("limit"), 100)
	if err != nil {
		writeError(w, err)
		return
	}
	offset, err := parseOptionalInt(r.URL.Query().Get("offset"), 0)
	if err != nil {
		writeError(w, err)
		return
	}
	response, err := h.dictionary.List(r.Context(), user.ID, r.URL.Query().Get("filter"), r.URL.Query().Get("q"), limit, offset)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, response)
}

func (h *Handler) CreateDictionaryWord(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, err := currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	payload, err := decodeDictionaryUpsertPayload(r)
	if err != nil {
		writeError(w, err)
		return
	}
	entry, err := h.dictionary.Create(r.Context(), user, payload)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusCreated, entry)
}

func (h *Handler) UpdateDictionaryWord(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, err := currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	wordID, err := parseUUID(chi.URLParam(r, "wordID"))
	if err != nil {
		writeError(w, domain.ErrValidation)
		return
	}
	payload, err := decodeDictionaryUpsertPayload(r)
	if err != nil {
		writeError(w, err)
		return
	}
	entry, err := h.dictionary.Update(r.Context(), user, wordID, payload)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, entry)
}

func (h *Handler) DeleteDictionaryWord(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, err := currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	wordID, err := parseUUID(chi.URLParam(r, "wordID"))
	if err != nil {
		writeError(w, domain.ErrValidation)
		return
	}
	if err := h.dictionary.Delete(r.Context(), user.ID, wordID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) GetDailyPool(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, err := currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	view, err := h.pools.GetOrCreateDailyPool(r.Context(), user)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, view)
}

func (h *Handler) GetNextCard(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, err := currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	card, err := h.pools.GetNextCard(r.Context(), user)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, card)
}

func (h *Handler) SubmitFirstExposure(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, err := currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	itemID, err := parseUUID(chi.URLParam(r, "poolItemID"))
	if err != nil {
		writeError(w, domain.ErrValidation)
		return
	}
	var payload struct {
		Action         domain.ExposureAction `json:"action"`
		ResponseTimeMs int                   `json:"response_time_ms"`
		ClientEventID  string                `json:"client_event_id"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, domain.ErrValidation)
		return
	}
	if err := h.learning.SubmitFirstExposure(r.Context(), user, service.FirstExposureRequest{
		PoolItemID:     itemID,
		Action:         payload.Action,
		ResponseTimeMs: payload.ResponseTimeMs,
		ClientEventID:  payload.ClientEventID,
	}); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) SubmitReview(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, err := currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	itemID, err := parseUUID(chi.URLParam(r, "poolItemID"))
	if err != nil {
		writeError(w, domain.ErrValidation)
		return
	}
	var payload struct {
		Rating         domain.ReviewRating `json:"rating"`
		ModeUsed       domain.ReviewMode   `json:"mode_used"`
		ResponseTimeMs int                 `json:"response_time_ms"`
		ClientEventID  string              `json:"client_event_id"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, domain.ErrValidation)
		return
	}
	if err := h.learning.SubmitReview(r.Context(), user, service.ReviewRequest{
		PoolItemID:     itemID,
		Rating:         payload.Rating,
		ModeUsed:       payload.ModeUsed,
		ResponseTimeMs: payload.ResponseTimeMs,
		ClientEventID:  payload.ClientEventID,
	}); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) SubmitReveal(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, err := currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	itemID, err := parseUUID(chi.URLParam(r, "poolItemID"))
	if err != nil {
		writeError(w, domain.ErrValidation)
		return
	}
	var payload struct {
		Kind           domain.RevealKind `json:"kind"`
		ModeUsed       domain.ReviewMode `json:"mode_used"`
		ResponseTimeMs int               `json:"response_time_ms"`
		ClientEventID  string            `json:"client_event_id"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, domain.ErrValidation)
		return
	}
	if err := h.learning.SubmitReveal(r.Context(), user, service.RevealRequest{
		PoolItemID:     itemID,
		Kind:           payload.Kind,
		ModeUsed:       payload.ModeUsed,
		ResponseTimeMs: payload.ResponseTimeMs,
		ClientEventID:  payload.ClientEventID,
	}); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) SubmitPronunciation(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, err := currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	itemID, err := parseUUID(chi.URLParam(r, "poolItemID"))
	if err != nil {
		writeError(w, domain.ErrValidation)
		return
	}
	var payload struct {
		ClientEventID string `json:"client_event_id"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, domain.ErrValidation)
		return
	}
	if err := h.learning.SubmitPronunciation(r.Context(), user, service.PronunciationRequest{
		PoolItemID:    itemID,
		ClientEventID: payload.ClientEventID,
	}); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) AdminRebuildPool(w nethttp.ResponseWriter, r *nethttp.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeError(w, domain.ErrValidation)
		return
	}
	view, err := h.pools.ForceRebuildTodayPool(r.Context(), domain.User{ID: userID})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, view)
}

func (h *Handler) AdminListLLMRuns(w nethttp.ResponseWriter, r *nethttp.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeError(w, domain.ErrValidation)
		return
	}
	runs, err := h.llmRuns.ListRecentByUser(r.Context(), userID, 20)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]any{"runs": runs})
}

func decodeDictionaryUpsertPayload(r *nethttp.Request) (service.DictionaryUpsertInput, error) {
	var payload struct {
		Word               string                      `json:"word"`
		CanonicalForm      string                      `json:"canonical_form"`
		Lemma              string                      `json:"lemma"`
		WordFamily         string                      `json:"word_family"`
		ConfusableGroupKey string                      `json:"confusable_group_key"`
		PartOfSpeech       string                      `json:"part_of_speech"`
		Level              domain.CEFRLevel            `json:"level"`
		Topic              string                      `json:"topic"`
		IPA                string                      `json:"ipa"`
		PronunciationHint  string                      `json:"pronunciation_hint"`
		VietnameseMeaning  string                      `json:"vietnamese_meaning"`
		EnglishMeaning     string                      `json:"english_meaning"`
		ExampleSentence1   string                      `json:"example_sentence_1"`
		ExampleSentence2   string                      `json:"example_sentence_2"`
		ListStatus         domain.DictionaryListStatus `json:"list_status"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		return service.DictionaryUpsertInput{}, fmt.Errorf("%w: invalid json body", domain.ErrValidation)
	}
	return service.DictionaryUpsertInput{
		Word:               payload.Word,
		CanonicalForm:      payload.CanonicalForm,
		Lemma:              payload.Lemma,
		WordFamily:         payload.WordFamily,
		ConfusableGroupKey: payload.ConfusableGroupKey,
		PartOfSpeech:       payload.PartOfSpeech,
		Level:              payload.Level,
		Topic:              payload.Topic,
		IPA:                payload.IPA,
		PronunciationHint:  payload.PronunciationHint,
		VietnameseMeaning:  payload.VietnameseMeaning,
		EnglishMeaning:     payload.EnglishMeaning,
		ExampleSentence1:   payload.ExampleSentence1,
		ExampleSentence2:   payload.ExampleSentence2,
		ListStatus:         payload.ListStatus,
	}, nil
}

func parseOptionalInt(value string, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, domain.ErrValidation
	}
	return parsed, nil
}
