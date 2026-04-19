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
	clock                       Clock
	logger                      *slog.Logger
	memoryCauseInferenceEnabled bool
	maxGenerationAttempts       int
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
	shortTermStates, reviewStates, weakStates, comebackMode := capReviewPracticeStates(shortTermStates, reviewStates, weakStates)

	topic := TopicForDate(now.In(loc))
	newQuota := ComputeNewWordQuota(settings.DailyNewWordLimit, len(reviewStates), len(shortTermStates), weakSlots)
	if comebackMode {
		newQuota = catchUpNewQuota(settings.DailyNewWordLimit)
	}

	wordMap, err := s.loadWordMap(ctx, append(extractStateWordIDs(shortTermStates), append(extractStateWordIDs(reviewStates), extractStateWordIDs(weakStates)...)...))
	if err != nil {
		return DailyPoolView{}, err
	}

	items = buildReviewItems(user.ID, uuid.Nil, shortTermStates, wordMap, domain.PoolItemTypeShortTerm, s.memoryCauseInferenceEnabled)
	items = append(items, buildReviewItems(user.ID, uuid.Nil, reviewStates, wordMap, domain.PoolItemTypeReview, s.memoryCauseInferenceEnabled)...)
	items = append(items, buildReviewItems(user.ID, uuid.Nil, weakStates, wordMap, domain.PoolItemTypeWeak, s.memoryCauseInferenceEnabled)...)

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

func (s *PoolService) GetNextCard(ctx context.Context, user domain.User, sessionID string) (CardResponse, error) {
	view, err := s.GetOrCreateDailyPool(ctx, user)
	if err != nil {
		return CardResponse{}, err
	}
	now := s.clock.Now()
	settings, err := s.settingsRepo.Get(ctx, user.ID)
	if err != nil {
		return CardResponse{}, err
	}
	card, shouldReplenish, err := s.nextCardFromView(ctx, user.ID, view, now, sessionID, settings.DailyNewWordLimit)
	if err != nil {
		return CardResponse{}, err
	}
	if !shouldReplenish {
		return card, nil
	}

	if replenished, _, replenishErr := s.replenishUnknownDailySlots(ctx, user.ID, view.Pool, view.Items, now); replenishErr != nil {
		return CardResponse{}, replenishErr
	} else if replenished {
		view, err = s.GetOrCreateDailyPool(ctx, user)
		if err != nil {
			return CardResponse{}, err
		}
		card, shouldReplenish, err = s.nextCardFromView(ctx, user.ID, view, now, sessionID, settings.DailyNewWordLimit)
		if err != nil {
			return CardResponse{}, err
		}
		if !shouldReplenish {
			return card, nil
		}
	}

	if replenished, replenishErr := s.replenishBonusPracticeItems(ctx, user.ID, view.Pool, view.Items, now); replenishErr != nil {
		return CardResponse{}, replenishErr
	} else if replenished {
		view, err = s.GetOrCreateDailyPool(ctx, user)
		if err != nil {
			return CardResponse{}, err
		}
		card, _, err = s.nextCardFromView(ctx, user.ID, view, now, sessionID, settings.DailyNewWordLimit)
		if err != nil {
			return CardResponse{}, err
		}
		return card, nil
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
) (CardResponse, bool, error) {
	progress, err := s.buildSessionProgress(ctx, userID, sessionID, view.Pool, view.Items, now)
	if err != nil {
		return CardResponse{}, false, err
	}
	comebackMode := isComebackPool(view.Pool, view.Items)
	effectiveNewLimit := dailyNewWordLimit
	if comebackMode {
		effectiveNewLimit = catchUpNewQuota(dailyNewWordLimit)
	}
	item, nextDue, completeReason := findNextCardForSession(view.Items, now, progress, comebackMode, effectiveNewLimit)
	if item != nil || completeReason != "" {
		return buildCardResponse(view.Pool.LocalDate, progress, comebackMode, item, nextDue, completeReason), false, nil
	}
	if sessionID != "" && nextDue == nil {
		return buildCardResponse(view.Pool.LocalDate, progress, comebackMode, nil, nil, sessionCompleteReasonNoCards), false, nil
	}
	return buildCardResponse(view.Pool.LocalDate, progress, comebackMode, nil, nextDue, ""), true, nil
}

func buildCardResponse(
	localDate string,
	progress sessionProgress,
	comebackMode bool,
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
		DailyReviewCap:         progress.DailyReviewCap,
		DailyReviewCompleted:   progress.DailyReviewCompleted,
		SessionTotalCompleted:  progress.SessionTotalCompleted,
		SessionReviewCompleted: progress.SessionReviewCompleted,
		SessionNewCompleted:    progress.SessionNewCompleted,
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

	remainingReviewBudget := catchUpDailyReviewCap - totalReviewPracticeItems(items)
	if remainingReviewBudget <= 0 {
		return false, nil
	}
	limit := minInt(maxInt(ComputeWeakSlots(settings.DailyNewWordLimit), 1), remainingReviewBudget)
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
	for _, bonusItem := range buildBonusPracticeItems(userID, pool.ID, weakStates, wordMap, s.memoryCauseInferenceEnabled) {
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
) (bool, []uuid.UUID, error) {
	settings, err := s.settingsRepo.Get(ctx, userID)
	if err != nil {
		return false, nil, err
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

	newWords, _, _, err := s.generateNewWords(ctx, userID, settings, pool.Topic, bufferState.PrefetchBatchSize, items, now)
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
	comebackMode bool,
	effectiveNewLimit int,
) (*domain.DailyLearningPoolItem, *time.Time, string) {
	if progress.SessionComplete {
		return nil, nil, progress.SessionCompleteReason
	}

	reviewCapReached := progress.DailyReviewCompleted >= progress.DailyReviewCap
	newCapReached := comebackMode && effectiveNewLimit >= 0 && progress.DailyNewCompleted >= effectiveNewLimit
	var nextDue *time.Time
	reviewCandidates := make([]domain.DailyLearningPoolItem, 0)
	newCandidates := make([]domain.DailyLearningPoolItem, 0)
	for _, item := range items {
		if item.Status != domain.PoolItemStatusPending {
			continue
		}
		if item.DueAt != nil && item.DueAt.After(now) {
			if nextDue == nil || item.DueAt.Before(*nextDue) {
				nextDue = item.DueAt
			}
			continue
		}
		if IsReviewPracticeItem(item) {
			if !reviewCapReached {
				reviewCandidates = append(reviewCandidates, item)
			}
			continue
		}
		if item.ItemType == domain.PoolItemTypeNew && !newCapReached {
			newCandidates = append(newCandidates, item)
		}
	}

	reviewCandidate := bestActionableItem(reviewCandidates)
	newCandidate := bestActionableItem(newCandidates)
	if progress.SessionID == "" {
		if reviewCandidate != nil {
			return reviewCandidate, nil, ""
		}
		if newCandidate != nil {
			return newCandidate, nil, ""
		}
		return nil, nextDue, ""
	}

	if progress.PreferredKind == completedCardKindNew {
		if newCandidate != nil {
			return newCandidate, nil, ""
		}
		return nil, nil, sessionCompleteReasonNoNew
	}

	if reviewCandidate != nil {
		return reviewCandidate, nil, ""
	}
	if reviewCapReached {
		return nil, nil, sessionCompleteReasonDailyCap
	}
	if progress.SessionNewCompleted == 0 && newCandidate != nil {
		return newCandidate, nil, ""
	}
	if progress.SessionNewCompleted > 0 {
		return nil, nil, sessionCompleteReasonNoReview
	}
	return nil, nextDue, ""
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

func capReviewPracticeStates(
	shortTermStates []domain.UserWordState,
	reviewStates []domain.UserWordState,
	weakStates []domain.UserWordState,
) ([]domain.UserWordState, []domain.UserWordState, []domain.UserWordState, bool) {
	if totalDueReviewPracticeItems(shortTermStates, reviewStates, weakStates) <= catchUpDailyReviewCap {
		return shortTermStates, reviewStates, weakStates, false
	}
	remaining := catchUpDailyReviewCap
	shortTermStates = takeStates(shortTermStates, &remaining)
	reviewStates = takeStates(reviewStates, &remaining)
	weakStates = takeStates(weakStates, &remaining)
	return shortTermStates, reviewStates, weakStates, true
}

func takeStates(states []domain.UserWordState, remaining *int) []domain.UserWordState {
	if *remaining <= 0 {
		return nil
	}
	if len(states) <= *remaining {
		*remaining -= len(states)
		return states
	}
	out := states[:*remaining]
	*remaining = 0
	return out
}

func isComebackPool(pool domain.DailyLearningPool, items []domain.DailyLearningPoolItem) bool {
	if pool.ShortTermCount+pool.DueReviewCount+pool.WeakCount >= catchUpDailyReviewCap {
		return true
	}
	return totalReviewPracticeItems(items) >= catchUpDailyReviewCap
}

func catchUpNewQuota(dailyLimit int) int {
	if dailyLimit <= 0 {
		return 0
	}
	return minInt(dailyLimit, catchUpSessionNewRunCap)
}

func effectiveNewWordBufferLimits(dailyLimit int, comebackMode bool) (int, int) {
	if comebackMode {
		quota := catchUpNewQuota(dailyLimit)
		return quota, quota
	}
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
	remainingReviewBudget := catchUpDailyReviewCap - totalReviewPracticeItems(items)
	if remainingReviewBudget <= 0 {
		return false, nil
	}
	missingShort, missingReview = capMissingScheduledStates(missingShort, missingReview, remainingReviewBudget)
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

	appended := append(
		buildReviewItems(userID, pool.ID, missingShort, wordMap, domain.PoolItemTypeShortTerm, s.memoryCauseInferenceEnabled),
		buildReviewItems(userID, pool.ID, missingReview, wordMap, domain.PoolItemTypeReview, s.memoryCauseInferenceEnabled)...,
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

func capMissingScheduledStates(shortTermStates []domain.UserWordState, reviewStates []domain.UserWordState, budget int) ([]domain.UserWordState, []domain.UserWordState) {
	remaining := budget
	shortTermStates = takeStates(shortTermStates, &remaining)
	reviewStates = takeStates(reviewStates, &remaining)
	return shortTermStates, reviewStates
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
	newWords, _, _, err := s.generateNewWords(ctx, user.ID, settings, selectedTopic, settings.DailyNewWordLimit, view.Items, now)
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
		"topic", selectedTopic,
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
		if _, selected := selectedWordIDSet[word.ID]; selected {
			continue
		}
		selectedWords = append(selectedWords, word)
		selectedWordIDSet[word.ID] = struct{}{}
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

func buildReviewItems(userID uuid.UUID, poolID uuid.UUID, states []domain.UserWordState, words map[uuid.UUID]domain.Word, itemType domain.PoolItemType, memoryCauseInferenceEnabled bool) []domain.DailyLearningPoolItem {
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
			ReviewMode:            SelectReviewMode(state, memoryCauseInferenceEnabled),
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

func buildBonusPracticeItems(userID uuid.UUID, poolID uuid.UUID, states []domain.UserWordState, words map[uuid.UUID]domain.Word, memoryCauseInferenceEnabled bool) []domain.DailyLearningPoolItem {
	items := buildReviewItems(userID, poolID, states, words, domain.PoolItemTypeWeak, memoryCauseInferenceEnabled)
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
