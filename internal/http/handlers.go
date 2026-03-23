package apihttp

import (
	"errors"
	"fmt"
	"io"
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
	if h.dynamicReview != nil {
		if items, overlayErr := h.dynamicReview.OverlayPoolItems(r.Context(), user.ID, view.Pool.LocalDate, view.Items); overlayErr != nil {
			h.logger.Warn("overlay dynamic review prompts on daily pool", "user_id", user.ID, "local_date", view.Pool.LocalDate, "error", overlayErr)
		} else {
			view.Items = items
		}
	}
	writeJSON(w, nethttp.StatusOK, view)
}

func (h *Handler) AppendMoreWords(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, err := currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var payload struct {
		Topic string `json:"topic"`
	}
	if r.Body != nil {
		if err := decodeJSON(r, &payload); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, errors.New(domain.ErrValidation.Error()+": invalid json body"))
			return
		}
	}
	view, err := h.pools.AppendMoreNewWords(r.Context(), user, payload.Topic)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, view)
}

func (h *Handler) GenerateDynamicReviewPrompts(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, err := currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if h.dynamicReview == nil {
		writeError(w, errors.New("dynamic review service unavailable"))
		return
	}

	view, err := h.pools.GetOrCreateDailyPool(r.Context(), user)
	if err != nil {
		writeError(w, err)
		return
	}

	result, err := h.dynamicReview.Prewarm(r.Context(), user.ID, view.Pool.LocalDate, view.Items)
	if err != nil {
		writeError(w, err)
		return
	}

	switch {
	case result.EligibleCount == 0:
		result.Message = "No Mode 2/3 scheduled cards need dynamic prompts right now."
	case result.GeneratedCount == 0:
		result.Message = "Today's dynamic review prompts are already ready."
	default:
		result.Message = fmt.Sprintf("Generated %d dynamic review prompts for today.", result.GeneratedCount)
	}

	writeJSON(w, nethttp.StatusOK, result)
}

func (h *Handler) StartExercise(w nethttp.ResponseWriter, r *nethttp.Request) {
	user, err := currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	response, err := h.exercise.StartSession(r.Context(), user)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, response)
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
	if h.dynamicReview != nil && card.PoolItem != nil {
		enriched, hasPrompt, overlayErr := h.dynamicReview.OverlayCardOnly(r.Context(), user.ID, card.LocalDate, *card.PoolItem)
		if overlayErr != nil {
			h.logger.Warn("overlay dynamic review prompt on next card", "user_id", user.ID, "local_date", card.LocalDate, "word_id", card.PoolItem.WordID, "error", overlayErr)
		} else {
			card.PoolItem = &enriched
		}
		if !hasPrompt && (card.PoolItem.ReviewMode == domain.ReviewModeMultipleChoice || card.PoolItem.ReviewMode == domain.ReviewModeFillBlank) {
			if view, viewErr := h.pools.GetOrCreateDailyPool(r.Context(), user); viewErr != nil {
				h.logger.Warn("load daily pool for dynamic review backfill", "user_id", user.ID, "local_date", card.LocalDate, "error", viewErr)
			} else if backfillErr := h.dynamicReview.BackfillForCurrentCard(r.Context(), user.ID, view.Pool.LocalDate, view.Items, *card.PoolItem); backfillErr != nil {
				h.logger.Warn("backfill dynamic review prompt for next card", "user_id", user.ID, "local_date", view.Pool.LocalDate, "word_id", card.PoolItem.WordID, "error", backfillErr)
			} else if enriched, applied, overlayErr := h.dynamicReview.OverlayCardOnly(r.Context(), user.ID, card.LocalDate, *card.PoolItem); overlayErr != nil {
				h.logger.Warn("overlay dynamic review prompt after backfill", "user_id", user.ID, "local_date", card.LocalDate, "word_id", card.PoolItem.WordID, "error", overlayErr)
			} else if applied {
				card.PoolItem = &enriched
			}
		}
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
		Rating                           domain.ReviewRating      `json:"rating"`
		ModeUsed                         domain.ReviewMode        `json:"mode_used"`
		ResponseTimeMs                   int                      `json:"response_time_ms"`
		ClientEventID                    string                   `json:"client_event_id"`
		AnswerCorrect                    *bool                    `json:"answer_correct"`
		RevealedMeaningBeforeAnswer      bool                     `json:"revealed_meaning_before_answer"`
		RevealedExampleBeforeAnswer      bool                     `json:"revealed_example_before_answer"`
		UsedHint                         bool                     `json:"used_hint"`
		InputMethod                      domain.ReviewInputMethod `json:"input_method"`
		NormalizedTypedAnswer            string                   `json:"normalized_typed_answer"`
		SelectedChoiceWordID             string                   `json:"selected_choice_word_id"`
		SelectedChoiceConfusableGroupKey string                   `json:"selected_choice_confusable_group_key"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, domain.ErrValidation)
		return
	}
	var selectedChoiceWordID *uuid.UUID
	if payload.SelectedChoiceWordID != "" {
		parsed, parseErr := uuid.Parse(payload.SelectedChoiceWordID)
		if parseErr != nil {
			writeError(w, domain.ErrValidation)
			return
		}
		selectedChoiceWordID = &parsed
	}
	if err := h.learning.SubmitReview(r.Context(), user, service.ReviewRequest{
		PoolItemID:                       itemID,
		Rating:                           payload.Rating,
		ModeUsed:                         payload.ModeUsed,
		ResponseTimeMs:                   payload.ResponseTimeMs,
		ClientEventID:                    payload.ClientEventID,
		AnswerCorrect:                    payload.AnswerCorrect,
		RevealedMeaningBeforeAnswer:      payload.RevealedMeaningBeforeAnswer,
		RevealedExampleBeforeAnswer:      payload.RevealedExampleBeforeAnswer,
		UsedHint:                         payload.UsedHint,
		InputMethod:                      payload.InputMethod,
		NormalizedTypedAnswer:            payload.NormalizedTypedAnswer,
		SelectedChoiceWordID:             selectedChoiceWordID,
		SelectedChoiceConfusableGroupKey: payload.SelectedChoiceConfusableGroupKey,
	}); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nethttp.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) UndoLastAnswer(w nethttp.ResponseWriter, r *nethttp.Request) {
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
	if err := h.learning.UndoLastAnswer(r.Context(), user, service.UndoLastAnswerRequest{
		PoolItemID: itemID,
	}); err != nil {
		writeError(w, err)
		return
	}
	view, err := h.pools.GetOrCreateDailyPool(r.Context(), user)
	if err != nil {
		writeError(w, err)
		return
	}
	for idx := range view.Items {
		if view.Items[idx].ID == itemID {
			if h.dynamicReview != nil {
				if enriched, _, overlayErr := h.dynamicReview.OverlayCardOnly(r.Context(), user.ID, view.Pool.LocalDate, view.Items[idx]); overlayErr != nil {
					h.logger.Warn("overlay dynamic review prompt on undo", "user_id", user.ID, "local_date", view.Pool.LocalDate, "word_id", view.Items[idx].WordID, "error", overlayErr)
				} else {
					view.Items[idx] = enriched
				}
			}
			card := service.CardResponse{
				LocalDate: view.Pool.LocalDate,
				PoolItem:  &view.Items[idx],
			}
			writeJSON(w, nethttp.StatusOK, card)
			return
		}
	}
	writeError(w, fmt.Errorf("%w: reopened card was not found in the current pool", domain.ErrNotFound))
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
