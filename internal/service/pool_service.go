package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

type PoolService struct {
	settingsRepo                SettingsRepository
	wordRepo                    WordRepository
	stateRepo                   WordStateRepository
	poolRepo                    PoolRepository
	eventRepo                   LearningEventRepository
	llmRepo                     LLMRunRepository
	generator                   CandidateGenerator
	wordSets                    *WordSetService
	clock                       Clock
	logger                      *slog.Logger
	memoryCauseInferenceEnabled bool
	maxGenerationAttempts       int
}

// SetWordSetService wires the optional WordSetService dependency. It is
// optional so unit tests / older callers can construct the PoolService
// without word-set filtering; when nil, [FilterDailyPoolByActiveSet] is a
// no-op and the full daily pool is returned.
func (s *PoolService) SetWordSetService(wordSets *WordSetService) {
	s.wordSets = wordSets
}

type UnknownDailyBufferMutation struct {
	CreatedItemIDs         []uuid.UUID
	DeletedPendingNewItems []domain.DailyLearningPoolItem
}

type newWordBufferState struct {
	DailyLimit        int
	PrefetchBatchSize int
	LearnedNewCount   int
	PendingNewItems   []domain.DailyLearningPoolItem
}

type generatedItemKind string

const (
	generatedItemKindSingleWord  generatedItemKind = "single_word"
	generatedItemKindPhrasalVerb generatedItemKind = "phrasal_verb"
	generatedItemKindCollocation generatedItemKind = "collocation"
)

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
	memoryCauseInferenceEnabled bool,
) *PoolService {
	return &PoolService{
		settingsRepo:                settingsRepo,
		wordRepo:                    wordRepo,
		stateRepo:                   stateRepo,
		poolRepo:                    poolRepo,
		eventRepo:                   eventRepo,
		llmRepo:                     llmRepo,
		generator:                   generator,
		clock:                       clock,
		logger:                      logger,
		memoryCauseInferenceEnabled: memoryCauseInferenceEnabled,
		maxGenerationAttempts:       3,
	}
}

func (s *PoolService) GetOrCreateDailyPool(ctx context.Context, user domain.User) (DailyPoolView, error) {
	settings, err := s.settingsRepo.Get(ctx, user.ID)
	if err != nil {
		return DailyPoolView{}, err
	}

	now := s.clock.Now()
	localDate, _, endUTC, loc, err := domain.BoundsForLocalDate(now, settings.Timezone)
	if err != nil {
		return DailyPoolView{}, err
	}
	if deleted, pruneErr := s.poolRepo.DeletePendingItemsBeforeLocalDate(ctx, user.ID, localDate); pruneErr != nil {
		s.logger.Warn("prune stale pending pool items", "user_id", user.ID, "local_date", localDate, "error", pruneErr)
	} else if deleted > 0 {
		s.logger.Info("pruned stale pending pool items", "user_id", user.ID, "local_date", localDate, "deleted_count", deleted)
	}

	pool, items, err := s.poolRepo.GetByLocalDate(ctx, user.ID, localDate)
	if err == nil {
		reconciled, recErr := s.reconcileScheduledPoolItems(ctx, user.ID, pool, items, endUTC)
		if recErr != nil {
			return DailyPoolView{}, recErr
		}
		if reconciled {
			pool, items, err = s.poolRepo.GetByLocalDate(ctx, user.ID, localDate)
			if err != nil {
				return DailyPoolView{}, err
			}
		}
		if syncErr := s.syncPendingPoolItems(ctx, user.ID, items); syncErr != nil {
			return DailyPoolView{}, syncErr
		}
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

	shortTermStates, reviewStates, err := s.listScheduledDueStates(ctx, user.ID, endUTC)
	if err != nil {
		return DailyPoolView{}, err
	}
	excludeIDs := collectStateWordIDs(shortTermStates, reviewStates)
	weakSlots := ComputeWeakSlots(settings.DailyNewWordLimit)
	weakStates, err := s.stateRepo.ListWeakCandidates(ctx, user.ID, excludeIDs, weakSlots)
	if err != nil {
		return DailyPoolView{}, err
	}
	rawReviewPracticeCount := totalDueReviewPracticeItems(shortTermStates, reviewStates, weakStates)
	comebackMode := hasOverdueReviewStates(shortTermStates, reviewStates, weakStates, now)

	topic := TopicForDate(now.In(loc))
	newQuota := ComputeNewWordQuota(settings.DailyNewWordLimit, len(reviewStates), len(shortTermStates), weakSlots)

	wordMap, err := s.loadWordMap(ctx, append(extractStateWordIDs(shortTermStates), append(extractStateWordIDs(reviewStates), extractStateWordIDs(weakStates)...)...))
	if err != nil {
		return DailyPoolView{}, err
	}

	reviewModesByWord, err := s.enabledReviewModesForStates(ctx, user.ID, shortTermStates, reviewStates, weakStates)
	if err != nil {
		return DailyPoolView{}, err
	}
	items = buildReviewItems(user.ID, uuid.Nil, shortTermStates, wordMap, domain.PoolItemTypeShortTerm, s.memoryCauseInferenceEnabled, reviewModesByWord)
	items = append(items, buildReviewItems(user.ID, uuid.Nil, reviewStates, wordMap, domain.PoolItemTypeReview, s.memoryCauseInferenceEnabled, reviewModesByWord)...)
	items = append(items, buildReviewItems(user.ID, uuid.Nil, weakStates, wordMap, domain.PoolItemTypeWeak, s.memoryCauseInferenceEnabled, reviewModesByWord)...)
	capListeningReviewItems(items, dailyListeningItemLimit, reviewModesByWord)

	if s.wordSets != nil {
		defaultSet, defaultErr := s.wordSets.EnsureDefault(ctx, user.ID)
		if defaultErr != nil {
			return DailyPoolView{}, defaultErr
		}
		if !defaultSet.AutoGenerateNewWords {
			newQuota = 0
		}
	}
	newWords, acceptedWords, rejectionSummary, err := s.generateNewWords(ctx, user.ID, settings, settings.CEFRLevel, topic, newQuota, items, now)
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
			"local_date":                localDate,
			"topic":                     topic,
			"due_review_count":          len(reviewStates),
			"short_term_count":          len(shortTermStates),
			"weak_count":                len(weakStates),
			"new_count":                 len(newWords),
			"comeback_mode":             comebackMode,
			"raw_review_practice_count": rawReviewPracticeCount,
			"accepted_new_words":        acceptedWords,
			"rejections":                rejectionSummary,
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
		"raw_review_practice_count", rawReviewPracticeCount,
		"comeback_mode", comebackMode,
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

func (s *PoolService) syncPendingPoolItems(ctx context.Context, userID uuid.UUID, items []domain.DailyLearningPoolItem) error {
	states := make(map[uuid.UUID]domain.UserWordState)
	for _, item := range items {
		if item.Status != domain.PoolItemStatusPending || !item.IsReview || item.FirstExposureRequired {
			continue
		}
		state, err := s.stateRepo.Get(ctx, userID, item.WordID)
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return err
		}
		states[item.WordID] = state
	}
	stateValues := make([]domain.UserWordState, 0, len(states))
	for _, state := range states {
		stateValues = append(stateValues, state)
	}
	reviewModesByWord, err := s.enabledReviewModesForStates(ctx, userID, stateValues)
	if err != nil {
		return err
	}
	// Listening (mode 5) is capped per day. Non-pending review items already
	// consumed part of that budget; the remainder is what pending items may use.
	listeningBudget := dailyListeningItemLimit
	for index := range items {
		item := items[index]
		if item.IsReview && item.Status != domain.PoolItemStatusPending && item.ReviewMode == domain.ReviewModeListening {
			listeningBudget--
		}
	}

	for index := range items {
		item := items[index]
		if item.Status != domain.PoolItemStatusPending || !item.IsReview || item.FirstExposureRequired {
			continue
		}

		state, ok := states[item.WordID]
		if !ok {
			continue
		}

		updated, _ := syncPendingPoolItem(item, state, s.memoryCauseInferenceEnabled, reviewModesByWord[item.WordID])
		if updated.ReviewMode == domain.ReviewModeListening {
			if listeningBudget > 0 {
				listeningBudget--
			} else {
				updated.ReviewMode = selectEnabledReviewMode(domain.ReviewModeFillBlank, reviewModesByWord[item.WordID], item.Word)
			}
		}
		changed := updated.ReviewMode != item.ReviewMode ||
			!sameOptionalTime(updated.DueAt, item.DueAt) ||
			jsonMapFloat64(item.Metadata, "weakness_score") != state.WeaknessScore
		if !changed {
			continue
		}
		if err := s.poolRepo.UpdatePendingPoolItem(ctx, updated); err != nil {
			return err
		}
		items[index] = updated
	}
	return nil
}

func syncPendingPoolItem(item domain.DailyLearningPoolItem, state domain.UserWordState, memoryCauseInferenceEnabled bool, configuredModes ...[]domain.ReviewMode) (domain.DailyLearningPoolItem, bool) {
	enabledModes := allReviewModes()
	if len(configuredModes) > 0 && len(configuredModes[0]) > 0 {
		enabledModes = configuredModes[0]
	}
	updated := item
	updated.ReviewMode = selectConfiguredReviewMode(state, memoryCauseInferenceEnabled, enabledModes, item.Word)
	if item.BonusPractice {
		updated.DueAt = nil
	} else {
		updated.DueAt = state.NextReviewAt
	}
	updated.Metadata = cloneJSONMap(item.Metadata)
	if updated.Metadata == nil {
		updated.Metadata = domain.JSONMap{}
	}
	updated.Metadata["weakness_score"] = state.WeaknessScore

	changed := updated.ReviewMode != item.ReviewMode || !sameOptionalTime(updated.DueAt, item.DueAt)
	if !changed && jsonMapFloat64(item.Metadata, "weakness_score") != state.WeaknessScore {
		changed = true
	}
	return updated, changed
}

func cloneJSONMap(value domain.JSONMap) domain.JSONMap {
	if value == nil {
		return nil
	}
	cloned := make(domain.JSONMap, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}

func sameOptionalTime(left *time.Time, right *time.Time) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return left.Equal(*right)
	}
}

func jsonMapFloat64(value domain.JSONMap, key string) float64 {
	if value == nil {
		return 0
	}
	raw, ok := value[key]
	if !ok {
		return 0
	}
	switch typed := raw.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	default:
		return 0
	}
}

func (s *PoolService) GetNextCard(ctx context.Context, user domain.User, sessionID string, practiceRequested bool) (CardResponse, error) {
	view, err := s.GetOrCreateDailyPool(ctx, user)
	if err != nil {
		return CardResponse{}, err
	}
	now := s.clock.Now()
	settings, err := s.settingsRepo.Get(ctx, user.ID)
	if err != nil {
		return CardResponse{}, err
	}
	// Replenishment decisions below need the unfiltered view (so the default
	// new_words set keeps topping up new words even when the user is on a
	// custom set). Only the card-selection step sees the filtered view.
	visibleView, filterErr := s.FilterDailyPoolByActiveSet(ctx, user, view)
	if filterErr != nil {
		s.logger.Warn("filter daily pool by active set for next card", "user_id", user.ID, "error", filterErr)
		visibleView = view
	}
	selectionView := visibleView
	card, err := s.nextCardFromView(ctx, user.ID, selectionView, now, sessionID, settings.DailyNewWordLimit, practiceRequested)
	if err != nil {
		return CardResponse{}, err
	}

	if practiceRequested {
		if card.PoolItem != nil || card.PendingPracticeCount > 0 {
			return card, nil
		}
		if replenished, replenishErr := s.replenishBonusPracticeItems(ctx, user.ID, view.Pool, view.Items, now); replenishErr != nil {
			return CardResponse{}, replenishErr
		} else if !replenished {
			return card, nil
		}
	} else {
		if card.SessionComplete && card.PendingPracticeCount > 0 {
			return card, nil
		}
		if sessionID == "" && card.PoolItem == nil && !card.SessionComplete {
			if replenished, _, replenishErr := s.replenishUnknownDailySlots(ctx, user.ID, view.Pool, view.Items, now); replenishErr != nil {
				return CardResponse{}, replenishErr
			} else if replenished {
				view, err = s.GetOrCreateDailyPool(ctx, user)
				if err != nil {
					return CardResponse{}, err
				}
				visibleView, filterErr = s.FilterDailyPoolByActiveSet(ctx, user, view)
				if filterErr != nil {
					visibleView = view
				}
				selectionView = visibleView
				card, err = s.nextCardFromView(ctx, user.ID, selectionView, now, sessionID, settings.DailyNewWordLimit, practiceRequested)
				if err != nil {
					return CardResponse{}, err
				}
				if card.PoolItem != nil || (card.SessionComplete && card.PendingPracticeCount > 0) {
					return card, nil
				}
			}
		}
		if !card.SessionComplete || card.PendingPracticeCount > 0 {
			return card, nil
		}
		if replenished, replenishErr := s.replenishBonusPracticeItems(ctx, user.ID, view.Pool, view.Items, now); replenishErr != nil {
			return CardResponse{}, replenishErr
		} else if !replenished {
			return card, nil
		}
	}

	view, err = s.GetOrCreateDailyPool(ctx, user)
	if err != nil {
		return CardResponse{}, err
	}
	visibleView, filterErr = s.FilterDailyPoolByActiveSet(ctx, user, view)
	if filterErr != nil {
		visibleView = view
	}
	selectionView = visibleView
	card, err = s.nextCardFromView(ctx, user.ID, selectionView, now, sessionID, settings.DailyNewWordLimit, practiceRequested)
	if err != nil {
		return CardResponse{}, err
	}
	return card, nil
}

func (s *PoolService) nextCardFromView(
	ctx context.Context,
	userID uuid.UUID,
	view DailyPoolView,
	now time.Time,
	sessionID string,
	dailyNewWordLimit int,
	practiceRequested bool,
) (CardResponse, error) {
	progress, err := s.buildSessionProgress(ctx, userID, sessionID, view.Pool, view.Items, now)
	if err != nil {
		return CardResponse{}, err
	}
	comebackMode := isComebackPool(view.Pool, view.Items)
	effectiveNewLimit := dailyNewWordLimit
	pendingDue := actionableItemsRemaining(view.Items, now, progress.DailyNewCompleted, effectiveNewLimit)
	pendingPractice := practiceItemsRemaining(view.Items, now)
	item, nextDue, completeReason := findNextCardForSession(view.Items, now, progress, comebackMode, effectiveNewLimit)
	if nextDue == nil {
		nextDue = collectSelectableSessionCandidates(view.Items, now, progress.DailyNewCompleted, effectiveNewLimit).nextDue
	}
	if practiceRequested && item == nil && completeReason == "" {
		item, completeReason = findNextPracticeCardForSession(view.Items, now, progress)
		if item != nil {
			nextDue = nil
		}
	}
	if item != nil || completeReason != "" {
		return buildCardResponse(view.Pool.LocalDate, progress, comebackMode, pendingDue, pendingPractice, item, nextDue, completeReason), nil
	}
	if sessionID != "" {
		return buildCardResponse(view.Pool.LocalDate, progress, comebackMode, pendingDue, pendingPractice, nil, nextDue, sessionCompleteReasonNoCards), nil
	}
	return buildCardResponse(view.Pool.LocalDate, progress, comebackMode, pendingDue, pendingPractice, nil, nextDue, ""), nil
}

func buildCardResponse(
	localDate string,
	progress sessionProgress,
	comebackMode bool,
	pendingDueCount int,
	pendingPracticeCount int,
	item *domain.DailyLearningPoolItem,
	nextDue *time.Time,
	completeReason string,
) CardResponse {
	sessionComplete := progress.SessionComplete
	if completeReason != "" {
		sessionComplete = true
	} else {
		completeReason = progress.SessionCompleteReason
	}
	return CardResponse{
		CardType:               domain.LearnCardTypePoolItem,
		LocalDate:              localDate,
		SessionID:              progress.SessionID,
		SessionComplete:        sessionComplete,
		SessionCompleteReason:  completeReason,
		ComebackMode:           comebackMode,
		SessionTotalCap:        progress.SessionTotalCap,
		DailyReviewCompleted:   progress.DailyReviewCompleted,
		SessionTotalCompleted:  progress.SessionTotalCompleted,
		SessionReviewCompleted: progress.SessionReviewCompleted,
		SessionNewCompleted:    progress.SessionNewCompleted,
		PendingDueCount:        pendingDueCount,
		PendingPracticeCount:   pendingPracticeCount,
		NextDueAt:              nextDue,
		PoolItem:               item,
	}
}

func (s *PoolService) ReconcileUnknownDailyBuffer(ctx context.Context, user domain.User) (UnknownDailyBufferMutation, error) {
	view, err := s.GetOrCreateDailyPool(ctx, user)
	if err != nil {
		return UnknownDailyBufferMutation{}, err
	}
	return s.reconcileUnknownDailyBuffer(ctx, user.ID, view.Pool, view.Items)
}

func (s *PoolService) MaintainNewWordBuffer(ctx context.Context, user domain.User) (UnknownDailyBufferMutation, error) {
	view, err := s.GetOrCreateDailyPool(ctx, user)
	if err != nil {
		return UnknownDailyBufferMutation{}, err
	}
	return s.maintainNewWordBuffer(ctx, user.ID, view.Pool, view.Items, s.clock.Now())
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

	reviewModesByWord, err := s.enabledReviewModesForStates(ctx, userID, weakStates)
	if err != nil {
		return false, err
	}
	appended := 0
	for _, bonusItem := range buildBonusPracticeItems(userID, pool.ID, weakStates, wordMap, s.memoryCauseInferenceEnabled, reviewModesByWord) {
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

	activeSet, err := s.resolvePracticeActiveSet(ctx, userID)
	if err != nil {
		return nil, err
	}

	history := extractBonusPracticeHistory(items)
	seenTodayWordIDs := bonusPracticeHistoryWordIDs(history)
	fetchLimit := limit
	if activeSet != nil {
		fetchLimit = maxInt(limit*4, limit+5)
	}
	freshCandidates, err := s.stateRepo.ListWeakCandidates(ctx, userID, seenTodayWordIDs, fetchLimit)
	if err != nil {
		return nil, err
	}
	freshCandidates = s.filterStatesByActiveSet(freshCandidates, activeSet)
	if len(freshCandidates) >= limit {
		return freshCandidates[:limit], nil
	}

	remaining := limit - len(freshCandidates)
	recycleExcludeWordIDs := extractStateWordIDs(freshCandidates)
	recycledCandidates, err := s.recycleBonusPracticeCandidates(ctx, userID, history, recycleExcludeWordIDs, remaining, activeSet)
	if err != nil {
		return nil, err
	}

	collected := append(freshCandidates, recycledCandidates...)
	if len(collected) >= limit || activeSet == nil {
		return collected, nil
	}

	// Cram fallback: when the active set has no weak / recently-practiced words
	// left to surface, let the user keep practicing by pulling the set's
	// remaining learning/review members regardless of their due date. This is
	// what makes "Start practice" work for a custom set (e.g. a speaking set)
	// that has members but nothing currently due. Bonus-practice ratings still
	// do not disturb the real review schedule.
	cramExcludeWordIDs := extractStateWordIDs(collected)
	cramExcludeWordIDs = append(cramExcludeWordIDs, poolItemWordIDs(items)...)
	cramCandidates, err := s.listSetCramCandidates(ctx, userID, activeSet, cramExcludeWordIDs, limit-len(collected))
	if err != nil {
		return nil, err
	}
	return append(collected, cramCandidates...), nil
}

// listSetCramCandidates returns learning/review word-states that belong to the
// active set, ignoring due date, for a free practice (cram) session. Words in
// excludeWordIDs (already-selected candidates and words already present in
// today's pool) are skipped to avoid duplicates.
func (s *PoolService) listSetCramCandidates(
	ctx context.Context,
	userID uuid.UUID,
	activeSet *domain.WordSet,
	excludeWordIDs []uuid.UUID,
	limit int,
) ([]domain.UserWordState, error) {
	if limit <= 0 || activeSet == nil {
		return nil, nil
	}

	states, err := s.stateRepo.ListExistingWords(ctx, userID)
	if err != nil {
		return nil, err
	}

	excluded := make(map[uuid.UUID]struct{}, len(excludeWordIDs))
	for _, wordID := range excludeWordIDs {
		excluded[wordID] = struct{}{}
	}

	candidates := make([]domain.UserWordState, 0, limit)
	for _, state := range states {
		if _, skip := excluded[state.WordID]; skip {
			continue
		}
		if state.Status != domain.WordStatusLearning && state.Status != domain.WordStatusReview {
			continue
		}
		if !stateMatchesActiveSet(state, activeSet) {
			continue
		}
		candidates = append(candidates, state)
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].WeaknessScore != candidates[j].WeaknessScore {
			return candidates[i].WeaknessScore > candidates[j].WeaknessScore
		}
		left, right := candidates[i].NextReviewAt, candidates[j].NextReviewAt
		if left != nil && right != nil && !left.Equal(*right) {
			return left.Before(*right)
		}
		if (left == nil) != (right == nil) {
			return left != nil
		}
		return candidates[i].WordID.String() < candidates[j].WordID.String()
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, nil
}

func (s *PoolService) recycleBonusPracticeCandidates(
	ctx context.Context,
	userID uuid.UUID,
	history map[uuid.UUID]bonusPracticeHistoryEntry,
	excludeWordIDs []uuid.UUID,
	limit int,
	activeSet *domain.WordSet,
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
		if !stateMatchesActiveSet(state, activeSet) {
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

func (s *PoolService) resolvePracticeActiveSet(ctx context.Context, userID uuid.UUID) (*domain.WordSet, error) {
	if s.wordSets == nil {
		return nil, nil
	}
	activeSet, err := s.wordSets.ResolveActiveSet(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &activeSet, nil
}

func (s *PoolService) filterStatesByActiveSet(states []domain.UserWordState, activeSet *domain.WordSet) []domain.UserWordState {
	if activeSet == nil {
		return states
	}
	filtered := make([]domain.UserWordState, 0, len(states))
	for _, state := range states {
		if stateMatchesActiveSet(state, activeSet) {
			filtered = append(filtered, state)
		}
	}
	return filtered
}

func stateMatchesActiveSet(state domain.UserWordState, activeSet *domain.WordSet) bool {
	if activeSet == nil {
		return true
	}
	if state.WordSetID == nil {
		return activeSet.IsDefault
	}
	return *state.WordSetID == activeSet.ID
}

func (s *PoolService) replenishUnknownDailySlots(
	ctx context.Context,
	userID uuid.UUID,
	pool domain.DailyLearningPool,
	items []domain.DailyLearningPoolItem,
	now time.Time,
) (bool, []uuid.UUID, error) {
	settings, err := s.settingsRepo.Get(ctx, userID)
	if err != nil {
		return false, nil, err
	}
	if s.wordSets != nil {
		defaultSet, setErr := s.wordSets.EnsureDefault(ctx, userID)
		if setErr != nil {
			return false, nil, setErr
		}
		if !defaultSet.AutoGenerateNewWords {
			return false, nil, nil
		}
	}
	dailyLimit, prefetchBatchSize := effectiveNewWordBufferLimits(settings.DailyNewWordLimit, isComebackPool(pool, items))
	bufferState, err := s.inspectNewWordBufferState(ctx, userID, dailyLimit, prefetchBatchSize, items)
	if err != nil {
		return false, nil, err
	}
	if bufferState.PrefetchBatchSize <= 0 || bufferState.LearnedNewCount >= bufferState.DailyLimit || len(bufferState.PendingNewItems) > 0 {
		return false, nil, nil
	}

	s.logger.Info("replenishing unknown daily slots at pool end",
		"user_id", userID,
		"pool_id", pool.ID,
		"local_date", pool.LocalDate,
		"daily_new_word_limit", settings.DailyNewWordLimit,
		"learned_new_count", bufferState.LearnedNewCount,
		"pending_new_count", len(bufferState.PendingNewItems),
		"prefetch_batch_size", bufferState.PrefetchBatchSize,
	)

	newWords, _, _, err := s.generateNewWords(ctx, userID, settings, settings.CEFRLevel, pool.Topic, bufferState.PrefetchBatchSize, items, now)
	if err != nil {
		return false, nil, err
	}
	if len(newWords) == 0 {
		return false, nil, fmt.Errorf("unable to replenish buffered daily slots: no replacement words generated")
	}

	lastOrdinal, err := s.poolRepo.GetLastOrdinal(ctx, pool.ID)
	if err != nil {
		return false, nil, err
	}
	newItems := buildNewItems(userID, pool.ID, newWords)
	createdItemIDs := make([]uuid.UUID, 0, len(newItems))
	for i := range newItems {
		newItems[i].Ordinal = lastOrdinal + i + 1
		appendedItem, err := s.poolRepo.AppendPoolItem(ctx, newItems[i])
		if err != nil {
			return false, nil, err
		}
		createdItemIDs = append(createdItemIDs, appendedItem.ID)
	}
	if err := s.poolRepo.IncrementNewCount(ctx, pool.ID, len(newItems)); err != nil {
		return false, nil, err
	}

	s.logger.Info("replenished unknown daily slots",
		"user_id", userID,
		"pool_id", pool.ID,
		"local_date", pool.LocalDate,
		"appended_new_items", len(newItems),
	)
	return true, createdItemIDs, nil
}

func (s *PoolService) reconcileUnknownDailyBuffer(
	ctx context.Context,
	userID uuid.UUID,
	pool domain.DailyLearningPool,
	items []domain.DailyLearningPoolItem,
) (UnknownDailyBufferMutation, error) {
	settings, err := s.settingsRepo.Get(ctx, userID)
	if err != nil {
		return UnknownDailyBufferMutation{}, err
	}
	dailyLimit, prefetchBatchSize := effectiveNewWordBufferLimits(settings.DailyNewWordLimit, isComebackPool(pool, items))
	bufferState, err := s.inspectNewWordBufferState(ctx, userID, dailyLimit, prefetchBatchSize, items)
	if err != nil {
		return UnknownDailyBufferMutation{}, err
	}
	if bufferState.DailyLimit <= 0 || bufferState.LearnedNewCount < bufferState.DailyLimit || len(bufferState.PendingNewItems) == 0 {
		return UnknownDailyBufferMutation{}, nil
	}

	deletedItems := copyPoolItems(bufferState.PendingNewItems)
	if err := s.poolRepo.DeletePoolItems(ctx, userID, extractPoolItemIDs(bufferState.PendingNewItems)); err != nil {
		return UnknownDailyBufferMutation{}, err
	}

	s.logger.Info("trimmed overflow pending new items after reaching daily limit",
		"user_id", userID,
		"pool_id", pool.ID,
		"local_date", pool.LocalDate,
		"daily_new_word_limit", bufferState.DailyLimit,
		"learned_new_count", bufferState.LearnedNewCount,
		"trimmed_pending_new_count", len(deletedItems),
	)
	return UnknownDailyBufferMutation{
		DeletedPendingNewItems: deletedItems,
	}, nil
}

func (s *PoolService) maintainNewWordBuffer(
	ctx context.Context,
	userID uuid.UUID,
	pool domain.DailyLearningPool,
	items []domain.DailyLearningPoolItem,
	now time.Time,
) (UnknownDailyBufferMutation, error) {
	settings, err := s.settingsRepo.Get(ctx, userID)
	if err != nil {
		return UnknownDailyBufferMutation{}, err
	}
	dailyLimit, prefetchBatchSize := effectiveNewWordBufferLimits(settings.DailyNewWordLimit, isComebackPool(pool, items))
	bufferState, err := s.inspectNewWordBufferState(ctx, userID, dailyLimit, prefetchBatchSize, items)
	if err != nil {
		return UnknownDailyBufferMutation{}, err
	}

	if bufferState.DailyLimit <= 0 || bufferState.LearnedNewCount >= bufferState.DailyLimit {
		return s.reconcileUnknownDailyBuffer(ctx, userID, pool, items)
	}
	if bufferState.PrefetchBatchSize <= 0 || len(bufferState.PendingNewItems) > 0 {
		return UnknownDailyBufferMutation{}, nil
	}

	replenished, createdItemIDs, err := s.replenishUnknownDailySlots(ctx, userID, pool, items, now)
	if err != nil {
		return UnknownDailyBufferMutation{}, err
	}
	if !replenished {
		return UnknownDailyBufferMutation{}, nil
	}
	return UnknownDailyBufferMutation{
		CreatedItemIDs: createdItemIDs,
	}, nil
}

func (s *PoolService) inspectNewWordBufferState(
	ctx context.Context,
	userID uuid.UUID,
	dailyLimit int,
	prefetchBatchSize int,
	items []domain.DailyLearningPoolItem,
) (newWordBufferState, error) {
	bufferState := newWordBufferState{
		DailyLimit:        dailyLimit,
		PrefetchBatchSize: prefetchBatchSize,
	}
	for _, item := range items {
		if item.ItemType != domain.PoolItemTypeNew {
			continue
		}
		if item.Status == domain.PoolItemStatusPending {
			bufferState.PendingNewItems = append(bufferState.PendingNewItems, copyPoolItem(item))
			continue
		}
		wordState, err := s.stateRepo.Get(ctx, userID, item.WordID)
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return newWordBufferState{}, err
		}
		if wordState.Status != domain.WordStatusKnown {
			bufferState.LearnedNewCount++
		}
	}
	return bufferState, nil
}

func findNextCardInItems(items []domain.DailyLearningPoolItem, now time.Time) (*domain.DailyLearningPoolItem, *time.Time) {
	var nextDue *time.Time
	var selected *domain.DailyLearningPoolItem
	selectedPriority := 0
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

		priority := poolItemPriority(item.ItemType)
		if selected == nil {
			copyItem := item
			selected = &copyItem
			selectedPriority = priority
			continue
		}
		if priority < selectedPriority || (priority == selectedPriority && compareActionableItems(item, *selected) < 0) {
			copyItem := item
			selected = &copyItem
			selectedPriority = priority
		}
	}
	if selected != nil {
		return selected, nil
	}
	return nil, nextDue
}

func findNextCardForSession(
	items []domain.DailyLearningPoolItem,
	now time.Time,
	progress sessionProgress,
	_ bool,
	effectiveNewLimit int,
) (*domain.DailyLearningPoolItem, *time.Time, string) {
	if progress.SessionComplete {
		return nil, nil, progress.SessionCompleteReason
	}

	candidates := collectSelectableSessionCandidates(items, now, progress.DailyNewCompleted, effectiveNewLimit)

	reviewCandidate := bestActionableItem(candidates.review)
	newCandidate := bestActionableItem(candidates.new)
	if progress.SessionID == "" {
		if reviewCandidate != nil {
			return reviewCandidate, nil, ""
		}
		if newCandidate != nil {
			return newCandidate, nil, ""
		}
		return nil, candidates.nextDue, ""
	}

	if progress.PreferredKind == completedCardKindNew {
		if newCandidate != nil {
			return newCandidate, nil, ""
		}
		if reviewCandidate != nil {
			return reviewCandidate, nil, ""
		}
		return nil, candidates.nextDue, ""
	}

	// Preferred kind is review
	if reviewCandidate != nil {
		return reviewCandidate, nil, ""
	}
	if newCandidate != nil {
		return newCandidate, nil, ""
	}
	return nil, candidates.nextDue, ""
}

func findNextPracticeCardForSession(
	items []domain.DailyLearningPoolItem,
	now time.Time,
	progress sessionProgress,
) (*domain.DailyLearningPoolItem, string) {
	if progress.SessionComplete {
		return nil, progress.SessionCompleteReason
	}
	candidates := collectSelectablePracticeCandidates(items, now)
	if len(candidates.items) == 0 {
		return nil, ""
	}
	return bestActionableItem(candidates.items), ""
}

func bestActionableItem(items []domain.DailyLearningPoolItem) *domain.DailyLearningPoolItem {
	if len(items) == 0 {
		return nil
	}
	selected := items[0]
	selectedPriority := poolItemPriority(selected.ItemType)
	for _, item := range items[1:] {
		priority := poolItemPriority(item.ItemType)
		if priority < selectedPriority || (priority == selectedPriority && compareActionableItems(item, selected) < 0) {
			selected = item
			selectedPriority = priority
		}
	}
	copyItem := selected
	return &copyItem
}

// comebackPoolReviewThreshold is the review-volume threshold (per day) that
// marks the pool as being in "comeback mode". It is a UI/throttling hint only
// and no longer caps the number of reviews a user can do in a day.
const comebackPoolReviewThreshold = 40

// hasOverdueReviewStates reports whether any due review state has a
// NextReviewAt before "now" (i.e. carried over from a previous day). Kept for
// pool generation logging.
func hasOverdueReviewStates(
	shortTermStates []domain.UserWordState,
	reviewStates []domain.UserWordState,
	weakStates []domain.UserWordState,
	now time.Time,
) bool {
	for _, group := range [][]domain.UserWordState{shortTermStates, reviewStates, weakStates} {
		for _, state := range group {
			if state.NextReviewAt != nil && state.NextReviewAt.Before(now) {
				return true
			}
		}
	}
	return false
}

// isComebackPool reports whether the user has accumulated enough due review
// items to be considered in "comeback mode". Mirrors the original cap-based
// trigger (≥40 review items) but no longer enforces it as a hard cap.
func isComebackPool(pool domain.DailyLearningPool, items []domain.DailyLearningPoolItem) bool {
	if pool.ShortTermCount+pool.DueReviewCount+pool.WeakCount >= comebackPoolReviewThreshold {
		return true
	}
	return totalReviewPracticeItems(items) >= comebackPoolReviewThreshold
}

func effectiveNewWordBufferLimits(dailyLimit int, _ bool) (int, int) {
	return dailyLimit, ComputeNewWordPrefetchBatchSize(dailyLimit)
}

func (s *PoolService) listScheduledDueStates(ctx context.Context, userID uuid.UUID, endUTC time.Time) ([]domain.UserWordState, []domain.UserWordState, error) {
	shortTermStates, err := s.stateRepo.ListDueWithinWindow(ctx, userID, time.Time{}, endUTC, true)
	if err != nil {
		return nil, nil, err
	}
	reviewStates, err := s.stateRepo.ListDueWithinWindow(ctx, userID, time.Time{}, endUTC, false)
	if err != nil {
		return nil, nil, err
	}
	return shortTermStates, reviewStates, nil
}

func (s *PoolService) reconcileScheduledPoolItems(
	ctx context.Context,
	userID uuid.UUID,
	pool domain.DailyLearningPool,
	items []domain.DailyLearningPoolItem,
	endUTC time.Time,
) (bool, error) {
	shortTermStates, reviewStates, err := s.listScheduledDueStates(ctx, userID, endUTC)
	if err != nil {
		return false, err
	}

	existing := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.ItemType != domain.PoolItemTypeShortTerm && item.ItemType != domain.PoolItemTypeReview {
			continue
		}
		existing[scheduledPoolKey(item.WordID, item.ItemType)] = struct{}{}
	}

	missingShort := filterMissingScheduledStates(shortTermStates, domain.PoolItemTypeShortTerm, existing)
	missingReview := filterMissingScheduledStates(reviewStates, domain.PoolItemTypeReview, existing)
	if len(missingShort) == 0 && len(missingReview) == 0 {
		return false, nil
	}

	wordIDs := append(extractStateWordIDs(missingShort), extractStateWordIDs(missingReview)...)
	wordMap, err := s.loadWordMap(ctx, wordIDs)
	if err != nil {
		return false, err
	}

	lastOrdinal, err := s.poolRepo.GetLastOrdinal(ctx, pool.ID)
	if err != nil {
		return false, err
	}

	reviewModesByWord, err := s.enabledReviewModesForStates(ctx, userID, missingShort, missingReview)
	if err != nil {
		return false, err
	}
	appended := append(
		buildReviewItems(userID, pool.ID, missingShort, wordMap, domain.PoolItemTypeShortTerm, s.memoryCauseInferenceEnabled, reviewModesByWord),
		buildReviewItems(userID, pool.ID, missingReview, wordMap, domain.PoolItemTypeReview, s.memoryCauseInferenceEnabled, reviewModesByWord)...,
	)
	for index := range appended {
		appended[index].Ordinal = lastOrdinal + index + 1
		if _, err := s.poolRepo.AppendPoolItem(ctx, appended[index]); err != nil {
			return false, err
		}
	}
	if err := s.poolRepo.IncrementScheduledCounts(ctx, pool.ID, len(missingReview), len(missingShort)); err != nil {
		return false, err
	}

	s.logger.Info("reconciled scheduled pool items",
		"user_id", userID,
		"pool_id", pool.ID,
		"local_date", pool.LocalDate,
		"appended_short_term", len(missingShort),
		"appended_review", len(missingReview),
	)
	return true, nil
}

func filterMissingScheduledStates(states []domain.UserWordState, itemType domain.PoolItemType, existing map[string]struct{}) []domain.UserWordState {
	out := make([]domain.UserWordState, 0, len(states))
	for _, state := range states {
		if _, ok := existing[scheduledPoolKey(state.WordID, itemType)]; ok {
			continue
		}
		out = append(out, state)
	}
	return out
}

func scheduledPoolKey(wordID uuid.UUID, itemType domain.PoolItemType) string {
	return wordID.String() + "|" + string(itemType)
}

func compareActionableItems(left domain.DailyLearningPoolItem, right domain.DailyLearningPoolItem) int {
	if diff := compareOptionalActionableDueAt(left.DueAt, right.DueAt); diff != 0 {
		return diff
	}
	return compareInts(left.Ordinal, right.Ordinal)
}

func compareOptionalActionableDueAt(left *time.Time, right *time.Time) int {
	switch {
	case left == nil && right == nil:
		return 0
	case left == nil:
		return 1
	case right == nil:
		return -1
	case left.Before(*right):
		return -1
	case left.After(*right):
		return 1
	default:
		return 0
	}
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

func (s *PoolService) AppendMoreNewWords(ctx context.Context, user domain.User, topic string) (DailyPoolView, error) {
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
	selectedTopic := strings.TrimSpace(topic)
	if selectedTopic == "" {
		selectedTopic = view.Pool.Topic
	}
	appendAttemptCount, err := s.countAppendMoreWordsAttemptsForLocalDate(ctx, user.ID, view.Pool.LocalDate, settings.Timezone, now)
	if err != nil {
		return DailyPoolView{}, err
	}
	generationLevel := settings.CEFRLevel
	if appendAttemptCount >= 2 {
		generationLevel = elevatedCEFRLevel(settings.CEFRLevel)
	}
	newWords, _, _, err := s.generateNewWords(ctx, user.ID, settings, generationLevel, selectedTopic, settings.DailyNewWordLimit, view.Items, now)
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
	if err := s.eventRepo.Insert(ctx, domain.LearningEvent{
		UserID:    user.ID,
		EventType: domain.EventTypeAppendMoreWords,
		EventTime: now,
		Payload: domain.JSONMap{
			"local_date":            view.Pool.LocalDate,
			"topic":                 selectedTopic,
			"attempt_number":        appendAttemptCount + 1,
			"base_cefr_level":       settings.CEFRLevel,
			"generation_cefr_level": generationLevel,
			"appended_new_items":    len(newItems),
		},
	}); err != nil {
		s.logger.Warn("record append more words event", "user_id", user.ID, "local_date", view.Pool.LocalDate, "error", err)
	}

	s.logger.Info("appended more new words",
		"user_id", user.ID,
		"pool_id", view.Pool.ID,
		"local_date", view.Pool.LocalDate,
		"topic", selectedTopic,
		"append_attempt_count_before", appendAttemptCount,
		"generation_cefr_level", generationLevel,
		"requested_new_items", settings.DailyNewWordLimit,
		"appended_new_items", len(newItems),
	)
	updatedView, err := s.GetOrCreateDailyPool(ctx, user)
	if err != nil {
		return DailyPoolView{}, err
	}
	updatedView.AppendedNew = len(newItems)
	return updatedView, nil
}

func (s *PoolService) generateNewWords(
	ctx context.Context,
	userID uuid.UUID,
	settings domain.UserSettings,
	level domain.CEFRLevel,
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
	seenNewSet := uuidSet(seenNewIDs)
	existingStateWordIDSet := uuidSet(extractStateWordIDs(existingStates))
	seedPoolWordIDSet := uuidSet(extractPoolWordIDs(seedItems))
	selectedWordIDSet := map[uuid.UUID]struct{}{}
	selectedKindCounts := map[generatedItemKind]int{}
	kindTargets := computeGeneratedItemKindTargets(newQuota)

	selectedWords := []domain.Word{}
	acceptedNames := []string{}
	rejections := map[string][]string{}
	exclusionWords, exclusionLemmas, exclusionGroups := BuildGenerationExclusions(existingWords, existingStates, seedItems)
	promptExclusionWords, promptExclusionLemmas, promptExclusionGroups := BuildGenerationPromptExclusions(existingWords, existingStates, seedItems)
	var lastGenerationErr error

	bankExcludeWordIDs := append(extractStateWordIDs(existingStates), extractPoolWordIDs(seedItems)...)
	bankWords, err := s.wordRepo.ListBankWords(ctx, userID, level, topic, bankExcludeWordIDs, minInt(newQuota+5, 20))
	if err != nil {
		return nil, nil, nil, err
	}
	filteredBankWords := orderWordsByGeneratedMix(filterBankWords(bankWords, &exclusionWords, &exclusionLemmas, &exclusionGroups, seenNewIDs), kindTargets)
	for _, word := range filteredBankWords {
		if len(selectedWords) >= newQuota {
			break
		}
		if _, selected := selectedWordIDSet[word.ID]; selected {
			continue
		}
		selectedWords = append(selectedWords, word)
		selectedWordIDSet[word.ID] = struct{}{}
		selectedKindCounts[classifyWordKind(word)]++
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
			CEFRLevel:         level,
			Topic:             topic,
			RequestedCount:    requested,
			PreferredLanguage: settings.PreferredMeaningLanguage,
			ExcludeWords:      append([]string{}, promptExclusionWords...),
			ExcludeLemmas:     append([]string{}, promptExclusionLemmas...),
			ExcludeGroupKeys:  append([]string{}, promptExclusionGroups...),
			MixHint:           buildGeneratedItemMixHint(kindTargets, selectedKindCounts, remaining),
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
			Provider:         domain.DefaultLLMProvider,
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
			if errors.Is(genErr, domain.ErrRateLimited) {
				break
			}
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

		selectedBeforeAttempt := len(selectedWords)
		for _, candidate := range result.Accepted {
			word, upsertErr := s.wordRepo.UpsertWord(ctx, candidate)
			if upsertErr != nil {
				rejections[candidate.Word] = []string{upsertErr.Error()}
				continue
			}
			if _, seen := seenNewSet[word.ID]; seen {
				rejections[candidate.Word] = append(rejections[candidate.Word], "recent new duplicate after upsert")
				continue
			}
			if _, exists := existingStateWordIDSet[word.ID]; exists {
				rejections[candidate.Word] = append(rejections[candidate.Word], "existing word state duplicate after upsert")
				continue
			}
			if _, seeded := seedPoolWordIDSet[word.ID]; seeded {
				rejections[candidate.Word] = append(rejections[candidate.Word], "seed pool duplicate after upsert")
				continue
			}
			if _, selected := selectedWordIDSet[word.ID]; selected {
				rejections[candidate.Word] = append(rejections[candidate.Word], "selected duplicate after upsert")
				continue
			}
			if len(selectedWords) >= newQuota {
				continue
			}
			selectedWords = append(selectedWords, word)
			selectedWordIDSet[word.ID] = struct{}{}
			selectedKindCounts[classifyWordKind(word)]++
			acceptedNames = append(acceptedNames, word.Word)
			existingWords = append(existingWords, word)
			addNonEmptySlice(&exclusionWords, word.Word)
			addNonEmptySlice(&exclusionWords, word.CanonicalForm)
			addNonEmptySlice(&exclusionLemmas, word.Lemma)
			addNonEmptySlice(&exclusionGroups, word.ConfusableGroupKey)
			addNonEmptySlice(&promptExclusionWords, word.Word)
			addNonEmptySlice(&promptExclusionWords, word.CanonicalForm)
			addNonEmptySlice(&promptExclusionLemmas, word.Lemma)
			addNonEmptySlice(&promptExclusionGroups, word.ConfusableGroupKey)
		}
		if len(selectedWords) == selectedBeforeAttempt {
			break
		}
	}

	for _, word := range filteredBankWords {
		if len(selectedWords) >= newQuota {
			break
		}
		if _, selected := selectedWordIDSet[word.ID]; selected {
			continue
		}
		selectedWords = append(selectedWords, word)
		selectedWordIDSet[word.ID] = struct{}{}
		selectedKindCounts[classifyWordKind(word)]++
		acceptedNames = append(acceptedNames, word.Word)
	}

	if newQuota > 0 && len(selectedWords) == 0 && len(seedItems) == 0 {
		if lastGenerationErr != nil {
			return nil, nil, rejections, fmt.Errorf("unable to generate initial daily pool words: %w", lastGenerationErr)
		}
		return nil, nil, rejections, fmt.Errorf("unable to generate initial daily pool words: all candidates were rejected")
	}

	return selectedWords, acceptedNames, rejections, nil
}

func (s *PoolService) countAppendMoreWordsAttemptsForLocalDate(
	ctx context.Context,
	userID uuid.UUID,
	localDate string,
	timezone string,
	now time.Time,
) (int, error) {
	_, startUTC, endUTC, _, err := domain.BoundsForLocalDate(now, timezone)
	if err != nil {
		return 0, err
	}
	events, err := s.eventRepo.ListByUserTimeRange(ctx, userID, startUTC, endUTC)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, event := range events {
		if event.EventType != domain.EventTypeAppendMoreWords {
			continue
		}
		if eventLocalDate, _ := event.Payload["local_date"].(string); eventLocalDate != "" && eventLocalDate != localDate {
			continue
		}
		count++
	}
	return count, nil
}

func elevatedCEFRLevel(level domain.CEFRLevel) domain.CEFRLevel {
	switch level {
	case domain.CEFRB1:
		return domain.CEFRB2
	case domain.CEFRB2:
		return domain.CEFRC1
	case domain.CEFRC1:
		return domain.CEFRC2
	default:
		return level
	}
}

func computeGeneratedItemKindTargets(limit int) map[generatedItemKind]int {
	targets := map[generatedItemKind]int{
		generatedItemKindSingleWord:  limit,
		generatedItemKindPhrasalVerb: 0,
		generatedItemKindCollocation: 0,
	}
	if limit <= 0 {
		return targets
	}
	if limit >= 6 {
		targets[generatedItemKindPhrasalVerb] = 1
		targets[generatedItemKindCollocation] = 1
		targets[generatedItemKindSingleWord] = limit - 2
	} else if limit >= 4 {
		targets[generatedItemKindPhrasalVerb] = 1
		targets[generatedItemKindSingleWord] = limit - 1
	}
	return targets
}

func buildGeneratedItemMixHint(targets map[generatedItemKind]int, selected map[generatedItemKind]int, remaining int) string {
	if remaining <= 0 {
		return "none"
	}
	parts := make([]string, 0, 3)
	for _, kind := range []generatedItemKind{generatedItemKindSingleWord, generatedItemKindPhrasalVerb, generatedItemKindCollocation} {
		needed := targets[kind] - selected[kind]
		if needed > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", needed, kind))
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d practical single_word items", remaining)
	}
	return strings.Join(parts, ", ")
}

func orderWordsByGeneratedMix(words []domain.Word, targets map[generatedItemKind]int) []domain.Word {
	ordered := append([]domain.Word{}, words...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return generatedItemKindPriority(classifyWordKind(ordered[i]), targets) < generatedItemKindPriority(classifyWordKind(ordered[j]), targets)
	})
	return ordered
}

func generatedItemKindPriority(kind generatedItemKind, targets map[generatedItemKind]int) int {
	if targets[kind] <= 0 {
		return 10
	}
	switch kind {
	case generatedItemKindPhrasalVerb:
		return 0
	case generatedItemKindCollocation:
		return 1
	default:
		return 2
	}
}

func classifyWordKind(word domain.Word) generatedItemKind {
	return classifyGeneratedItemKind(word.Word, word.PartOfSpeech)
}

func classifyGeneratedItemKind(value string, partOfSpeech string) generatedItemKind {
	normalizedPOS := strings.ToLower(strings.TrimSpace(partOfSpeech))
	switch normalizedPOS {
	case "phrasal verb", "phrasal_verb":
		return generatedItemKindPhrasalVerb
	case "collocation":
		return generatedItemKindCollocation
	}
	normalized := strings.TrimSpace(value)
	if strings.Contains(normalized, " ") || strings.Contains(normalized, "-") {
		if strings.Contains(normalizedPOS, "verb") {
			return generatedItemKindPhrasalVerb
		}
		return generatedItemKindCollocation
	}
	return generatedItemKindSingleWord
}

func buildReviewItems(userID uuid.UUID, poolID uuid.UUID, states []domain.UserWordState, words map[uuid.UUID]domain.Word, itemType domain.PoolItemType, memoryCauseInferenceEnabled bool, configuredReviewModes ...map[uuid.UUID][]domain.ReviewMode) []domain.DailyLearningPoolItem {
	reviewModesByWord := map[uuid.UUID][]domain.ReviewMode{}
	if len(configuredReviewModes) > 0 {
		reviewModesByWord = configuredReviewModes[0]
	}
	items := make([]domain.DailyLearningPoolItem, 0, len(states))
	for _, state := range states {
		word := words[state.WordID]
		wordCopy := word
		enabledModes := reviewModesByWord[state.WordID]
		if len(enabledModes) == 0 {
			enabledModes = allReviewModes()
		}
		reviewMode := selectConfiguredReviewMode(state, memoryCauseInferenceEnabled, enabledModes, &word)
		dueAt := state.NextReviewAt
		items = append(items, domain.DailyLearningPoolItem{
			PoolID:                poolID,
			UserID:                userID,
			WordID:                state.WordID,
			ItemType:              itemType,
			ReviewMode:            reviewMode,
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

func selectEnabledReviewMode(preferred domain.ReviewMode, enabled []domain.ReviewMode, word *domain.Word) domain.ReviewMode {
	if len(enabled) == 0 {
		return domain.ReviewModeReveal
	}
	compatible := func(mode domain.ReviewMode) bool {
		return mode != domain.ReviewModeBuildWord || word == nil || classifyWordKind(*word) == generatedItemKindSingleWord
	}
	for _, mode := range enabled {
		if mode == preferred && compatible(mode) {
			return mode
		}
	}
	order := []domain.ReviewMode{
		domain.ReviewModeReveal,
		domain.ReviewModeDefinitionFirst,
		domain.ReviewModeMultipleChoice,
		domain.ReviewModeBuildWord,
		domain.ReviewModeFillBlank,
		domain.ReviewModeListening,
	}
	preferredRank := 0
	for index, mode := range order {
		if mode == preferred {
			preferredRank = index
			break
		}
	}
	best := domain.ReviewModeReveal
	bestDistance := len(order) + 1
	for index, candidate := range order {
		if !compatible(candidate) || !containsReviewMode(enabled, candidate) {
			continue
		}
		distance := index - preferredRank
		if distance < 0 {
			distance = -distance
		}
		if distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	return best
}

// selectConfiguredReviewMode lets Mode 6 alternate with Mode 1 whenever the
// SRS chooses a meaning-recall card.  It remains a normal fallback: if Mode 6
// is disabled, or if the SRS selected another exercise, existing behaviour is
// unchanged.  LastMode makes repeat reviews alternate rather than assigning a
// word permanently to one side of the pair.
func selectConfiguredReviewMode(state domain.UserWordState, memoryCauseInferenceEnabled bool, enabled []domain.ReviewMode, word *domain.Word) domain.ReviewMode {
	preferred := SelectReviewMode(state, memoryCauseInferenceEnabled)
	if preferred == domain.ReviewModeReveal &&
		containsReviewMode(enabled, domain.ReviewModeDefinitionFirst) &&
		state.LastMode != domain.ReviewModeDefinitionFirst {
		preferred = domain.ReviewModeDefinitionFirst
	}
	return selectEnabledReviewMode(preferred, enabled, word)
}

func containsReviewMode(modes []domain.ReviewMode, target domain.ReviewMode) bool {
	for _, mode := range modes {
		if mode == target {
			return true
		}
	}
	return false
}

// dailyListeningItemLimit caps how many listening_sentence (mode 5) review items
// a user can be served in a single day. Listening is the heaviest construction
// mode, so we keep it scarce (1-2/day) while fill_in_blank (mode 4) stays common.
const dailyListeningItemLimit = 2

// capListeningReviewItems downgrades listening_sentence review items beyond
// `limit` to fill_in_blank, mutating items in place. Items are processed in
// slice order so the earliest listening items keep the mode deterministically.
func capListeningReviewItems(items []domain.DailyLearningPoolItem, limit int, configuredReviewModes ...map[uuid.UUID][]domain.ReviewMode) {
	reviewModesByWord := map[uuid.UUID][]domain.ReviewMode{}
	if len(configuredReviewModes) > 0 {
		reviewModesByWord = configuredReviewModes[0]
	}
	remaining := limit
	for i := range items {
		if !items[i].IsReview || items[i].ReviewMode != domain.ReviewModeListening {
			continue
		}
		if remaining > 0 {
			remaining--
			continue
		}
		enabledModes := reviewModesByWord[items[i].WordID]
		if len(enabledModes) == 0 {
			enabledModes = allReviewModes()
		}
		items[i].ReviewMode = selectEnabledReviewMode(domain.ReviewModeFillBlank, enabledModes, items[i].Word)
	}
}

func buildBonusPracticeItems(userID uuid.UUID, poolID uuid.UUID, states []domain.UserWordState, words map[uuid.UUID]domain.Word, memoryCauseInferenceEnabled bool, reviewModesByWord map[uuid.UUID][]domain.ReviewMode) []domain.DailyLearningPoolItem {
	items := buildReviewItems(userID, poolID, states, words, domain.PoolItemTypeWeak, memoryCauseInferenceEnabled, reviewModesByWord)
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

func extractPoolItemIDs(items []domain.DailyLearningPoolItem) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		if item.ID == uuid.Nil {
			continue
		}
		out = append(out, item.ID)
	}
	return out
}

func copyPoolItems(items []domain.DailyLearningPoolItem) []domain.DailyLearningPoolItem {
	out := make([]domain.DailyLearningPoolItem, 0, len(items))
	for _, item := range items {
		out = append(out, copyPoolItem(item))
	}
	return out
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

func poolItemWordIDs(items []domain.DailyLearningPoolItem) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		out = append(out, item.WordID)
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

func uuidSet(values []uuid.UUID) map[uuid.UUID]struct{} {
	out := make(map[uuid.UUID]struct{}, len(values))
	for _, value := range values {
		if value == uuid.Nil {
			continue
		}
		out[value] = struct{}{}
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

func (s *PoolService) enabledReviewModesForStates(ctx context.Context, userID uuid.UUID, groups ...[]domain.UserWordState) (map[uuid.UUID][]domain.ReviewMode, error) {
	result := map[uuid.UUID][]domain.ReviewMode{}
	wordIDs := []uuid.UUID{}
	seen := map[uuid.UUID]struct{}{}
	for _, states := range groups {
		for _, state := range states {
			if _, exists := seen[state.WordID]; exists {
				continue
			}
			seen[state.WordID] = struct{}{}
			wordIDs = append(wordIDs, state.WordID)
		}
	}
	if len(wordIDs) == 0 {
		return result, nil
	}
	if s.wordSets == nil {
		for _, wordID := range wordIDs {
			result[wordID] = allReviewModes()
		}
		return result, nil
	}
	return s.wordSets.EnabledReviewModesForWords(ctx, userID, wordIDs)
}

// RemapPendingReviewModes applies a saved set preference without altering
// completed history. It is deliberately best-effort at the HTTP boundary: a
// later pool sync will retry if today's pool cannot be read right now.
func (s *PoolService) RemapPendingReviewModes(ctx context.Context, userID uuid.UUID, set domain.WordSet) error {
	settings, err := s.settingsRepo.Get(ctx, userID)
	if err != nil {
		return err
	}
	localDate, _, _, _, err := domain.BoundsForLocalDate(s.clock.Now(), settings.Timezone)
	if err != nil {
		return err
	}
	_, items, err := s.poolRepo.GetByLocalDate(ctx, userID, localDate)
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	wordIDs := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		if item.Status == domain.PoolItemStatusPending && item.IsReview && !item.FirstExposureRequired {
			wordIDs = append(wordIDs, item.WordID)
		}
	}
	setIDs, err := s.stateRepo.GetWordSetIDsForWords(ctx, userID, wordIDs)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Status != domain.PoolItemStatusPending || !item.IsReview || item.FirstExposureRequired || setIDs[item.WordID] != set.ID {
			continue
		}
		state, stateErr := s.stateRepo.Get(ctx, userID, item.WordID)
		if stateErr != nil {
			if isNotFound(stateErr) {
				continue
			}
			return stateErr
		}
		updated, changed := syncPendingPoolItem(item, state, s.memoryCauseInferenceEnabled, set.EnabledReviewModes)
		if changed {
			if err := s.poolRepo.UpdatePendingPoolItem(ctx, updated); err != nil {
				return err
			}
		}
	}
	return nil
}

// FilterDailyPoolByActiveSet returns a view restricted to pool items whose
// underlying user_word_state belongs to the user's currently active word set.
// New-word generation continues to run unfiltered in the background and
// always targets the user's new_words-mode set, but when the user is in any
// other (custom) set, those new-word cards must not appear in their session.
//
// The filter is applied in-memory after [GetOrCreateDailyPool] so that the
// stored pool stays "global" per (user, local_date) and switching sets only
// changes what the client sees — not what is generated.
//
// When the PoolService was constructed without a [WordSetService] (e.g. in
// tests), this method is a no-op and returns the original view unchanged.
func (s *PoolService) FilterDailyPoolByActiveSet(ctx context.Context, user domain.User, view DailyPoolView) (DailyPoolView, error) {
	if s.wordSets == nil {
		return view, nil
	}
	if len(view.Items) == 0 {
		return view, nil
	}
	activeSet, err := s.wordSets.ResolveActiveSet(ctx, user.ID)
	if err != nil {
		return view, err
	}
	wordIDs := make([]uuid.UUID, 0, len(view.Items))
	seen := map[uuid.UUID]struct{}{}
	for _, item := range view.Items {
		if _, ok := seen[item.WordID]; ok {
			continue
		}
		seen[item.WordID] = struct{}{}
		wordIDs = append(wordIDs, item.WordID)
	}
	setMap, err := s.stateRepo.GetWordSetIDsForWords(ctx, user.ID, wordIDs)
	if err != nil {
		return view, err
	}
	filtered := make([]domain.DailyLearningPoolItem, 0, len(view.Items))
	var due, short, weak, newCount int
	for _, item := range view.Items {
		setID, hasMapping := setMap[item.WordID]
		// Items whose state has no word_set_id (legacy rows that escaped the
		// backfill, e.g. created after migration but before the default set
		// existed) are conservatively shown only when the active set is the
		// default — otherwise they'd be invisible across all sets.
		if !hasMapping {
			if !activeSet.IsDefault {
				continue
			}
		} else if setID != activeSet.ID {
			continue
		}
		filtered = append(filtered, item)
		switch item.ItemType {
		case domain.PoolItemTypeReview:
			due++
		case domain.PoolItemTypeShortTerm:
			short++
		case domain.PoolItemTypeWeak:
			weak++
		case domain.PoolItemTypeNew:
			newCount++
		}
	}
	out := view
	out.Items = filtered
	out.Counts = domain.PoolGenerationCounts{
		DueReview: due,
		ShortTerm: short,
		Weak:      weak,
		New:       newCount,
	}
	return out, nil
}
