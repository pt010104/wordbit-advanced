package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

type PoolService struct {
	settingsRepo          SettingsRepository
	wordRepo              WordRepository
	stateRepo             WordStateRepository
	poolRepo              PoolRepository
	eventRepo             LearningEventRepository
	llmRepo               LLMRunRepository
	generator             CandidateGenerator
	clock                 Clock
	logger                *slog.Logger
	maxGenerationAttempts int
}

func NewPoolService(
	settingsRepo SettingsRepository,
	wordRepo WordRepository,
	stateRepo WordStateRepository,
	poolRepo PoolRepository,
	eventRepo LearningEventRepository,
	llmRepo LLMRunRepository,
	generator CandidateGenerator,
	clock Clock,
	logger *slog.Logger,
) *PoolService {
	return &PoolService{
		settingsRepo:          settingsRepo,
		wordRepo:              wordRepo,
		stateRepo:             stateRepo,
		poolRepo:              poolRepo,
		eventRepo:             eventRepo,
		llmRepo:               llmRepo,
		generator:             generator,
		clock:                 clock,
		logger:                logger,
		maxGenerationAttempts: 3,
	}
}

func (s *PoolService) GetOrCreateDailyPool(ctx context.Context, user domain.User) (DailyPoolView, error) {
	settings, err := s.settingsRepo.Get(ctx, user.ID)
	if err != nil {
		return DailyPoolView{}, err
	}

	now := s.clock.Now()
	localDate, startUTC, endUTC, loc, err := domain.BoundsForLocalDate(now, settings.Timezone)
	if err != nil {
		return DailyPoolView{}, err
	}

	pool, items, err := s.poolRepo.GetByLocalDate(ctx, user.ID, localDate)
	if err == nil {
		return DailyPoolView{
			Pool:  pool,
			Items: items,
			Counts: domain.PoolGenerationCounts{
				DueReview: pool.DueReviewCount,
				ShortTerm: pool.ShortTermCount,
				Weak:      pool.WeakCount,
				New:       pool.NewCount,
			},
		}, nil
	}
	if err != nil && !isNotFound(err) {
		return DailyPoolView{}, err
	}

	if err := s.poolRepo.AcquireDailyPoolLock(ctx, user.ID, localDate); err != nil {
		return DailyPoolView{}, err
	}
	pool, items, err = s.poolRepo.GetByLocalDate(ctx, user.ID, localDate)
	if err == nil {
		return DailyPoolView{
			Pool:  pool,
			Items: items,
			Counts: domain.PoolGenerationCounts{
				DueReview: pool.DueReviewCount,
				ShortTerm: pool.ShortTermCount,
				Weak:      pool.WeakCount,
				New:       pool.NewCount,
			},
		}, nil
	}
	if err != nil && !isNotFound(err) {
		return DailyPoolView{}, err
	}

	shortTermStates, err := s.stateRepo.ListDueWithinWindow(ctx, user.ID, startUTC, endUTC, true)
	if err != nil {
		return DailyPoolView{}, err
	}
	reviewStates, err := s.stateRepo.ListDueWithinWindow(ctx, user.ID, startUTC, endUTC, false)
	if err != nil {
		return DailyPoolView{}, err
	}
	excludeIDs := collectStateWordIDs(shortTermStates, reviewStates)
	weakSlots := ComputeWeakSlots(settings.DailyNewWordLimit)
	weakStates, err := s.stateRepo.ListWeakCandidates(ctx, user.ID, excludeIDs, weakSlots)
	if err != nil {
		return DailyPoolView{}, err
	}

	topic := TopicForDate(now.In(loc))
	newQuota := ComputeNewWordQuota(settings.DailyNewWordLimit, len(reviewStates), len(shortTermStates), weakSlots)

	wordMap, err := s.loadWordMap(ctx, append(extractStateWordIDs(shortTermStates), append(extractStateWordIDs(reviewStates), extractStateWordIDs(weakStates)...)...))
	if err != nil {
		return DailyPoolView{}, err
	}

	items = buildReviewItems(user.ID, uuid.Nil, shortTermStates, wordMap, domain.PoolItemTypeShortTerm)
	items = append(items, buildReviewItems(user.ID, uuid.Nil, reviewStates, wordMap, domain.PoolItemTypeReview)...)
	items = append(items, buildReviewItems(user.ID, uuid.Nil, weakStates, wordMap, domain.PoolItemTypeWeak)...)

	newWords, acceptedWords, rejectionSummary, err := s.generateNewWords(ctx, user.ID, settings, topic, newQuota, items, now)
	if err != nil {
		return DailyPoolView{}, err
	}
	items = append(items, buildNewItems(user.ID, uuid.Nil, newWords)...)

	assignOrdinals(items)
	pool = domain.DailyLearningPool{
		UserID:         user.ID,
		LocalDate:      localDate,
		Timezone:       settings.Timezone,
		Topic:          topic,
		DueReviewCount: len(reviewStates),
		ShortTermCount: len(shortTermStates),
		WeakCount:      len(weakStates),
		NewCount:       len(newWords),
		GeneratedAt:    now,
	}

	pool, items, err = s.poolRepo.CreatePoolWithItems(ctx, pool, items)
	if err != nil {
		return DailyPoolView{}, err
	}

	if err := s.eventRepo.Insert(ctx, domain.LearningEvent{
		UserID:    user.ID,
		EventType: domain.EventTypePoolGenerated,
		EventTime: now,
		Payload: domain.JSONMap{
			"local_date":         localDate,
			"topic":              topic,
			"due_review_count":   len(reviewStates),
			"short_term_count":   len(shortTermStates),
			"weak_count":         len(weakStates),
			"new_count":          len(newWords),
			"accepted_new_words": acceptedWords,
			"rejections":         rejectionSummary,
		},
	}); err != nil {
		s.logger.Warn("record pool generation event", "error", err)
	}

	s.logger.Info("daily pool generated",
		"user_id", user.ID,
		"local_date", localDate,
		"topic", topic,
		"due_review_count", len(reviewStates),
		"short_term_count", len(shortTermStates),
		"weak_count", len(weakStates),
		"new_quota", newQuota,
		"new_count", len(newWords),
		"item_count", len(items),
	)

	return DailyPoolView{
		Pool:  pool,
		Items: items,
		Counts: domain.PoolGenerationCounts{
			DueReview: len(reviewStates),
			ShortTerm: len(shortTermStates),
			Weak:      len(weakStates),
			New:       len(newWords),
		},
	}, nil
}

func (s *PoolService) GetNextCard(ctx context.Context, user domain.User) (CardResponse, error) {
	view, err := s.GetOrCreateDailyPool(ctx, user)
	if err != nil {
		return CardResponse{}, err
	}
	now := s.clock.Now()
	if item, _ := findNextCardInItems(view.Items, now); item != nil {
		return CardResponse{
			LocalDate: view.Pool.LocalDate,
			PoolItem:  item,
		}, nil
	} else if replenished, replenishErr := s.replenishUnknownDailySlots(ctx, user.ID, view.Pool, view.Items, now); replenishErr != nil {
		return CardResponse{}, replenishErr
	} else if replenished {
		view, err = s.GetOrCreateDailyPool(ctx, user)
		if err != nil {
			return CardResponse{}, err
		}
		if item, nextDue := findNextCardInItems(view.Items, now); item != nil {
			return CardResponse{
				LocalDate: view.Pool.LocalDate,
				PoolItem:  item,
			}, nil
		} else {
			return CardResponse{
				LocalDate: view.Pool.LocalDate,
				NextDueAt: nextDue,
			}, nil
		}
	} else if replenished, replenishErr := s.replenishBonusPracticeItems(ctx, user.ID, view.Pool, view.Items, now); replenishErr != nil {
		return CardResponse{}, replenishErr
	} else if replenished {
		view, err = s.GetOrCreateDailyPool(ctx, user)
		if err != nil {
			return CardResponse{}, err
		}
		if item, nextDue := findNextCardInItems(view.Items, now); item != nil {
			return CardResponse{
				LocalDate: view.Pool.LocalDate,
				PoolItem:  item,
			}, nil
		} else {
			return CardResponse{
				LocalDate: view.Pool.LocalDate,
				NextDueAt: nextDue,
			}, nil
		}
	} else {
		_, nextDue := findNextCardInItems(view.Items, now)
		return CardResponse{
			LocalDate: view.Pool.LocalDate,
			NextDueAt: nextDue,
		}, nil
	}
}

func (s *PoolService) EnsureUnknownDailyQuota(ctx context.Context, user domain.User) error {
	view, err := s.GetOrCreateDailyPool(ctx, user)
	if err != nil {
		return err
	}
	_, err = s.replenishUnknownDailySlots(ctx, user.ID, view.Pool, view.Items, s.clock.Now())
	return err
}

func (s *PoolService) replenishBonusPracticeItems(
	ctx context.Context,
	userID uuid.UUID,
	pool domain.DailyLearningPool,
	items []domain.DailyLearningPoolItem,
	now time.Time,
) (bool, error) {
	settings, err := s.settingsRepo.Get(ctx, userID)
	if err != nil {
		return false, err
	}

	limit := maxInt(ComputeWeakSlots(settings.DailyNewWordLimit), 1)
	weakStates, err := s.listBonusPracticeCandidates(ctx, userID, items, limit)
	if err != nil {
		return false, err
	}
	if len(weakStates) == 0 {
		return false, nil
	}

	wordMap, err := s.loadWordMap(ctx, extractStateWordIDs(weakStates))
	if err != nil {
		return false, err
	}
	lastOrdinal, err := s.poolRepo.GetLastOrdinal(ctx, pool.ID)
	if err != nil {
		return false, err
	}

	appended := 0
	for _, bonusItem := range buildBonusPracticeItems(userID, pool.ID, weakStates, wordMap) {
		bonusItem.Ordinal = lastOrdinal + appended + 1
		if _, err := s.poolRepo.AppendPoolItem(ctx, bonusItem); err != nil {
			return false, err
		}
		appended++
	}
	if appended == 0 {
		return false, nil
	}
	if err := s.poolRepo.IncrementWeakCount(ctx, pool.ID, appended); err != nil {
		return false, err
	}

	s.logger.Info("replenished bonus practice items",
		"user_id", userID,
		"pool_id", pool.ID,
		"local_date", pool.LocalDate,
		"appended_bonus_items", appended,
		"at", now,
	)
	return true, nil
}

func (s *PoolService) listBonusPracticeCandidates(
	ctx context.Context,
	userID uuid.UUID,
	items []domain.DailyLearningPoolItem,
	limit int,
) ([]domain.UserWordState, error) {
	if limit <= 0 {
		return nil, nil
	}

	history := extractBonusPracticeHistory(items)
	seenTodayWordIDs := bonusPracticeHistoryWordIDs(history)
	freshCandidates, err := s.stateRepo.ListWeakCandidates(ctx, userID, seenTodayWordIDs, limit)
	if err != nil {
		return nil, err
	}
	if len(freshCandidates) >= limit {
		return freshCandidates, nil
	}

	remaining := limit - len(freshCandidates)
	recycleExcludeWordIDs := extractStateWordIDs(freshCandidates)
	recycledCandidates, err := s.recycleBonusPracticeCandidates(ctx, userID, history, recycleExcludeWordIDs, remaining)
	if err != nil {
		return nil, err
	}

	return append(freshCandidates, recycledCandidates...), nil
}

func (s *PoolService) recycleBonusPracticeCandidates(
	ctx context.Context,
	userID uuid.UUID,
	history map[uuid.UUID]bonusPracticeHistoryEntry,
	excludeWordIDs []uuid.UUID,
	limit int,
) ([]domain.UserWordState, error) {
	if limit <= 0 || len(history) == 0 {
		return nil, nil
	}

	excluded := make(map[uuid.UUID]struct{}, len(excludeWordIDs))
	for _, wordID := range excludeWordIDs {
		excluded[wordID] = struct{}{}
	}

	candidates := make([]domain.UserWordState, 0, len(history))
	for wordID := range history {
		if _, skip := excluded[wordID]; skip {
			continue
		}
		state, err := s.stateRepo.Get(ctx, userID, wordID)
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return nil, err
		}
		if state.Status != domain.WordStatusLearning && state.Status != domain.WordStatusReview {
			continue
		}
		candidates = append(candidates, state)
	}

	sort.Slice(candidates, func(i, j int) bool {
		leftHistory := history[candidates[i].WordID]
		rightHistory := history[candidates[j].WordID]
		if leftHistory.latestOrdinal != rightHistory.latestOrdinal {
			return leftHistory.latestOrdinal < rightHistory.latestOrdinal
		}
		if candidates[i].WeaknessScore != candidates[j].WeaknessScore {
			return candidates[i].WeaknessScore > candidates[j].WeaknessScore
		}
		return candidates[i].WordID.String() < candidates[j].WordID.String()
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, nil
}

func (s *PoolService) replenishUnknownDailySlots(
	ctx context.Context,
	userID uuid.UUID,
	pool domain.DailyLearningPool,
	items []domain.DailyLearningPoolItem,
	now time.Time,
) (bool, error) {
	settings, err := s.settingsRepo.Get(ctx, userID)
	if err != nil {
		return false, err
	}
	additionalNeeded, unknownCount, pendingNewCount, err := s.computeUnknownDailyNeed(ctx, userID, settings.DailyNewWordLimit, items)
	if err != nil {
		return false, err
	}
	if additionalNeeded <= 0 {
		return false, nil
	}

	s.logger.Info("replenishing unknown daily slots at pool end",
		"user_id", userID,
		"pool_id", pool.ID,
		"local_date", pool.LocalDate,
		"daily_new_word_limit", settings.DailyNewWordLimit,
		"unknown_count", unknownCount,
		"pending_new_count", pendingNewCount,
		"additional_needed", additionalNeeded,
	)

	newWords, _, _, err := s.generateNewWords(ctx, userID, settings, pool.Topic, additionalNeeded, items, now)
	if err != nil {
		return false, err
	}
	if len(newWords) == 0 {
		return false, fmt.Errorf("unable to replenish known daily slots: no replacement words generated")
	}

	lastOrdinal, err := s.poolRepo.GetLastOrdinal(ctx, pool.ID)
	if err != nil {
		return false, err
	}
	newItems := buildNewItems(userID, pool.ID, newWords)
	for i := range newItems {
		newItems[i].Ordinal = lastOrdinal + i + 1
		if _, err := s.poolRepo.AppendPoolItem(ctx, newItems[i]); err != nil {
			return false, err
		}
	}
	if err := s.poolRepo.IncrementNewCount(ctx, pool.ID, len(newItems)); err != nil {
		return false, err
	}

	s.logger.Info("replenished unknown daily slots",
		"user_id", userID,
		"pool_id", pool.ID,
		"local_date", pool.LocalDate,
		"appended_new_items", len(newItems),
	)
	return true, nil
}

func (s *PoolService) computeUnknownDailyNeed(
	ctx context.Context,
	userID uuid.UUID,
	dailyLimit int,
	items []domain.DailyLearningPoolItem,
) (int, int, int, error) {
	unknownCount := 0
	pendingNewCount := 0
	for _, item := range items {
		if item.ItemType != domain.PoolItemTypeNew {
			continue
		}
		if item.Status == domain.PoolItemStatusPending {
			pendingNewCount++
			continue
		}
		state, err := s.stateRepo.Get(ctx, userID, item.WordID)
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return 0, 0, 0, err
		}
		if state.Status != domain.WordStatusKnown {
			unknownCount++
		}
	}
	additionalNeeded := dailyLimit - unknownCount - pendingNewCount
	if additionalNeeded < 0 {
		return 0, unknownCount, pendingNewCount, nil
	}
	return additionalNeeded, unknownCount, pendingNewCount, nil
}

func findNextCardInItems(items []domain.DailyLearningPoolItem, now time.Time) (*domain.DailyLearningPoolItem, *time.Time) {
	var nextDue *time.Time
	for i := range items {
		item := items[i]
		if item.Status != domain.PoolItemStatusPending {
			continue
		}
		if item.DueAt != nil && item.DueAt.After(now) {
			if nextDue == nil || item.DueAt.Before(*nextDue) {
				nextDue = item.DueAt
			}
			continue
		}
		copyItem := item
		return &copyItem, nil
	}
	return nil, nextDue
}

func (s *PoolService) ForceRebuildTodayPool(ctx context.Context, user domain.User) (DailyPoolView, error) {
	settings, err := s.settingsRepo.Get(ctx, user.ID)
	if err != nil {
		return DailyPoolView{}, err
	}
	localDate, _, _, _, err := domain.BoundsForLocalDate(s.clock.Now(), settings.Timezone)
	if err != nil {
		return DailyPoolView{}, err
	}
	if err := s.poolRepo.ForceDeleteByLocalDate(ctx, user.ID, localDate); err != nil && !isNotFound(err) {
		return DailyPoolView{}, err
	}
	return s.GetOrCreateDailyPool(ctx, user)
}

func (s *PoolService) AppendMoreNewWords(ctx context.Context, user domain.User) (DailyPoolView, error) {
	settings, err := s.settingsRepo.Get(ctx, user.ID)
	if err != nil {
		return DailyPoolView{}, err
	}
	view, err := s.GetOrCreateDailyPool(ctx, user)
	if err != nil {
		return DailyPoolView{}, err
	}
	if settings.DailyNewWordLimit <= 0 {
		return view, nil
	}

	now := s.clock.Now()
	newWords, _, _, err := s.generateNewWords(ctx, user.ID, settings, view.Pool.Topic, settings.DailyNewWordLimit, view.Items, now)
	if err != nil {
		return DailyPoolView{}, err
	}
	if len(newWords) == 0 {
		return DailyPoolView{}, fmt.Errorf("unable to append more words: no words generated")
	}

	lastOrdinal, err := s.poolRepo.GetLastOrdinal(ctx, view.Pool.ID)
	if err != nil {
		return DailyPoolView{}, err
	}
	newItems := buildNewItems(user.ID, view.Pool.ID, newWords)
	for i := range newItems {
		newItems[i].Ordinal = lastOrdinal + i + 1
		if _, err := s.poolRepo.AppendPoolItem(ctx, newItems[i]); err != nil {
			return DailyPoolView{}, err
		}
	}
	if err := s.poolRepo.IncrementNewCount(ctx, view.Pool.ID, len(newItems)); err != nil {
		return DailyPoolView{}, err
	}

	s.logger.Info("appended more new words",
		"user_id", user.ID,
		"pool_id", view.Pool.ID,
		"local_date", view.Pool.LocalDate,
		"requested_new_items", settings.DailyNewWordLimit,
		"appended_new_items", len(newItems),
	)
	return s.GetOrCreateDailyPool(ctx, user)
}

func (s *PoolService) generateNewWords(
	ctx context.Context,
	userID uuid.UUID,
	settings domain.UserSettings,
	topic string,
	newQuota int,
	seedItems []domain.DailyLearningPoolItem,
	now time.Time,
) ([]domain.Word, []string, map[string][]string, error) {
	if newQuota <= 0 {
		return nil, nil, map[string][]string{}, nil
	}

	existingStates, err := s.stateRepo.ListExistingWords(ctx, userID)
	if err != nil {
		return nil, nil, nil, err
	}
	existingWordMap, err := s.loadWordMap(ctx, extractStateWordIDs(existingStates))
	if err != nil {
		return nil, nil, nil, err
	}
	existingWords := mapValues(existingWordMap)
	seenNewIDs, err := s.wordRepo.ListWordIDsSeenAsNew(ctx, userID, SeenNewWordLookback(now))
	if err != nil {
		return nil, nil, nil, err
	}

	selectedWords := []domain.Word{}
	acceptedNames := []string{}
	rejections := map[string][]string{}
	exclusionWords, exclusionLemmas, exclusionGroups := BuildGenerationExclusions(existingWords, existingStates, seedItems)
	var lastGenerationErr error

	bankExcludeWordIDs := append(extractStateWordIDs(existingStates), extractPoolWordIDs(seedItems)...)
	bankWords, err := s.wordRepo.ListBankWords(ctx, userID, settings.CEFRLevel, topic, bankExcludeWordIDs, minInt(newQuota+5, 20))
	if err != nil {
		return nil, nil, nil, err
	}
	for _, word := range filterBankWords(bankWords, &exclusionWords, &exclusionLemmas, &exclusionGroups, seenNewIDs) {
		if len(selectedWords) >= newQuota {
			break
		}
		selectedWords = append(selectedWords, word)
		acceptedNames = append(acceptedNames, word.Word)
		existingWords = append(existingWords, word)
		addNonEmptySlice(&exclusionWords, word.Word)
		addNonEmptySlice(&exclusionWords, word.CanonicalForm)
		addNonEmptySlice(&exclusionLemmas, word.Lemma)
		addNonEmptySlice(&exclusionGroups, word.ConfusableGroupKey)
	}

	for attempt := 1; attempt <= s.maxGenerationAttempts && len(selectedWords) < newQuota; attempt++ {
		remaining := newQuota - len(selectedWords)
		requested := remaining + 5
		if requested > 10 {
			requested = 10
		}
		s.logger.Info("daily pool generation attempt",
			"user_id", userID,
			"topic", topic,
			"attempt", attempt,
			"requested_count", requested,
			"new_quota", newQuota,
			"selected_so_far", len(selectedWords),
		)
		input := GenerationInput{
			UserID:            userID,
			CEFRLevel:         settings.CEFRLevel,
			Topic:             topic,
			RequestedCount:    requested,
			PreferredLanguage: settings.PreferredMeaningLanguage,
			ExcludeWords:      append([]string{}, exclusionWords...),
			ExcludeLemmas:     append([]string{}, exclusionLemmas...),
			ExcludeGroupKeys:  append([]string{}, exclusionGroups...),
		}

		candidates, raw, genErr := s.generator.GenerateCandidates(ctx, input)
		result := FilterCandidates(candidates, existingStates, append(existingWords, selectedWords...), seenNewIDs, seedItems)
		for word, reasons := range result.Rejected {
			rejections[word] = reasons
		}

		var errMessage string
		status := domain.LLMRunStatusSuccess
		if genErr != nil {
			errMessage = genErr.Error()
			status = domain.LLMRunStatusFailed
		} else if len(result.Accepted) == 0 {
			status = domain.LLMRunStatusPartial
		}
		_ = s.llmRepo.Insert(ctx, domain.LLMGenerationRun{
			UserID:           userID,
			LocalDate:        now.Format("2006-01-02"),
			Topic:            topic,
			RequestedCount:   requested,
			AcceptedCount:    len(result.Accepted),
			Attempt:          attempt,
			Status:           status,
			Provider:         domain.DefaultGeminiProvider,
			Model:            "dynamic",
			Prompt:           "candidate generation",
			RawResponse:      domain.JSONMap{"text": raw},
			RejectionSummary: castRejections(rejections),
			ErrorMessage:     errMessage,
		})
		if genErr != nil {
			lastGenerationErr = genErr
			s.logger.Warn("daily pool generation attempt failed",
				"user_id", userID,
				"topic", topic,
				"attempt", attempt,
				"requested_count", requested,
				"new_quota", newQuota,
				"response_size", len(raw),
				"error", genErr,
			)
			continue
		}
		s.logger.Info("daily pool generation attempt completed",
			"user_id", userID,
			"topic", topic,
			"attempt", attempt,
			"requested_count", requested,
			"response_size", len(raw),
			"candidate_count", len(candidates),
			"accepted_count", len(result.Accepted),
			"rejected_count", len(result.Rejected),
		)

		for _, candidate := range result.Accepted {
			word, upsertErr := s.wordRepo.UpsertWord(ctx, candidate)
			if upsertErr != nil {
				rejections[candidate.Word] = []string{upsertErr.Error()}
				continue
			}
			if len(selectedWords) >= newQuota {
				continue
			}
			selectedWords = append(selectedWords, word)
			acceptedNames = append(acceptedNames, word.Word)
			existingWords = append(existingWords, word)
			addNonEmptySlice(&exclusionWords, word.Word)
			addNonEmptySlice(&exclusionWords, word.CanonicalForm)
			addNonEmptySlice(&exclusionLemmas, word.Lemma)
			addNonEmptySlice(&exclusionGroups, word.ConfusableGroupKey)
		}
	}

	if newQuota > 0 && len(selectedWords) == 0 && len(seedItems) == 0 {
		if lastGenerationErr != nil {
			return nil, nil, rejections, fmt.Errorf("unable to generate initial daily pool words: %w", lastGenerationErr)
		}
		return nil, nil, rejections, fmt.Errorf("unable to generate initial daily pool words: all candidates were rejected")
	}

	return selectedWords, acceptedNames, rejections, nil
}

func buildReviewItems(userID uuid.UUID, poolID uuid.UUID, states []domain.UserWordState, words map[uuid.UUID]domain.Word, itemType domain.PoolItemType) []domain.DailyLearningPoolItem {
	items := make([]domain.DailyLearningPoolItem, 0, len(states))
	for _, state := range states {
		word := words[state.WordID]
		wordCopy := word
		dueAt := state.NextReviewAt
		items = append(items, domain.DailyLearningPoolItem{
			PoolID:                poolID,
			UserID:                userID,
			WordID:                state.WordID,
			ItemType:              itemType,
			ReviewMode:            SelectReviewMode(state),
			DueAt:                 dueAt,
			Status:                domain.PoolItemStatusPending,
			IsReview:              true,
			FirstExposureRequired: false,
			Metadata: domain.JSONMap{
				"weakness_score": state.WeaknessScore,
			},
			Word: &wordCopy,
		})
	}
	return items
}

func buildBonusPracticeItems(userID uuid.UUID, poolID uuid.UUID, states []domain.UserWordState, words map[uuid.UUID]domain.Word) []domain.DailyLearningPoolItem {
	items := buildReviewItems(userID, poolID, states, words, domain.PoolItemTypeWeak)
	for i := range items {
		items[i].BonusPractice = true
		items[i].DueAt = nil
		if items[i].Metadata == nil {
			items[i].Metadata = domain.JSONMap{}
		}
		items[i].Metadata["bonus_practice"] = true
	}
	return items
}

func buildNewItems(userID uuid.UUID, poolID uuid.UUID, words []domain.Word) []domain.DailyLearningPoolItem {
	items := make([]domain.DailyLearningPoolItem, 0, len(words))
	for _, word := range words {
		wordCopy := word
		items = append(items, domain.DailyLearningPoolItem{
			PoolID:                poolID,
			UserID:                userID,
			WordID:                word.ID,
			ItemType:              domain.PoolItemTypeNew,
			ReviewMode:            domain.ReviewModeReveal,
			Status:                domain.PoolItemStatusPending,
			IsReview:              false,
			FirstExposureRequired: true,
			Word:                  &wordCopy,
		})
	}
	return items
}

func assignOrdinals(items []domain.DailyLearningPoolItem) {
	for i := range items {
		items[i].Ordinal = i + 1
	}
}

func collectStateWordIDs(collections ...[]domain.UserWordState) []uuid.UUID {
	set := map[uuid.UUID]struct{}{}
	for _, states := range collections {
		for _, state := range states {
			set[state.WordID] = struct{}{}
		}
	}
	return mapUUIDKeys(set)
}

func extractPoolWordIDs(items []domain.DailyLearningPoolItem) []uuid.UUID {
	set := map[uuid.UUID]struct{}{}
	for _, item := range items {
		set[item.WordID] = struct{}{}
	}
	return mapUUIDKeys(set)
}

func filterBankWords(
	words []domain.Word,
	exclusionWords *[]string,
	exclusionLemmas *[]string,
	exclusionGroups *[]string,
	seenNewIDs []uuid.UUID,
) []domain.Word {
	seenNewSet := make(map[uuid.UUID]struct{}, len(seenNewIDs))
	for _, id := range seenNewIDs {
		seenNewSet[id] = struct{}{}
	}

	filtered := make([]domain.Word, 0, len(words))
	for _, word := range words {
		if _, seen := seenNewSet[word.ID]; seen {
			continue
		}
		if containsNormalized(*exclusionWords, word.Word) {
			continue
		}
		if containsNormalized(*exclusionWords, word.CanonicalForm) {
			continue
		}
		if containsNormalized(*exclusionLemmas, word.Lemma) {
			continue
		}
		if containsNormalized(*exclusionGroups, word.ConfusableGroupKey) {
			continue
		}
		filtered = append(filtered, word)
		addNonEmptySlice(exclusionWords, word.Word)
		addNonEmptySlice(exclusionWords, word.CanonicalForm)
		addNonEmptySlice(exclusionLemmas, word.Lemma)
		addNonEmptySlice(exclusionGroups, word.ConfusableGroupKey)
	}
	return filtered
}

type bonusPracticeHistoryEntry struct {
	latestOrdinal int
}

func extractBonusPracticeHistory(items []domain.DailyLearningPoolItem) map[uuid.UUID]bonusPracticeHistoryEntry {
	history := make(map[uuid.UUID]bonusPracticeHistoryEntry)
	for _, item := range items {
		if item.BonusPractice {
			entry := history[item.WordID]
			if item.Ordinal > entry.latestOrdinal {
				entry.latestOrdinal = item.Ordinal
			}
			history[item.WordID] = entry
		}
	}
	return history
}

func bonusPracticeHistoryWordIDs(history map[uuid.UUID]bonusPracticeHistoryEntry) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(history))
	for wordID := range history {
		out = append(out, wordID)
	}
	return out
}

func extractStateWordIDs(states []domain.UserWordState) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(states))
	for _, state := range states {
		out = append(out, state.WordID)
	}
	return out
}

func mapUUIDKeys(values map[uuid.UUID]struct{}) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	return out
}

func (s *PoolService) loadWordMap(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]domain.Word, error) {
	words, err := s.wordRepo.ListWordsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]domain.Word, len(words))
	for _, word := range words {
		out[word.ID] = word
	}
	return out, nil
}

func mapValues(values map[uuid.UUID]domain.Word) []domain.Word {
	out := make([]domain.Word, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func castRejections(values map[string][]string) domain.JSONMap {
	out := domain.JSONMap{}
	for key, value := range values {
		out[key] = value
	}
	return out
}

func addNonEmptySlice(target *[]string, value string) {
	value = NormalizeWord(value)
	if value == "" {
		return
	}
	for _, existing := range *target {
		if existing == value {
			return
		}
	}
	*target = append(*target, value)
}

func isNotFound(err error) bool {
	return errors.Is(err, domain.ErrNotFound)
}

func containsNormalized(values []string, value string) bool {
	value = NormalizeWord(value)
	if value == "" {
		return false
	}
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
