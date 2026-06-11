package service

import (
	"math"
	"time"

	"wordbit-advanced-app/backend/internal/domain"
)

const (
	transitionMode2DifficultyThreshold  = 0.60
	transitionMode2WeaknessThreshold    = 1.05
	standardMode2DifficultyThreshold    = 0.78
	standardMode2WeaknessThreshold      = 1.75
	standardMode2WrongCountThreshold    = 3
	standardMode2MeaningRevealThreshold = 4
	advancedConstructionSuccessStreak   = 2
	advancedConstructionDifficultyMax   = 0.78
	advancedConstructionWeaknessMax     = 1.75
	wordConstructionDefaultHintLimit    = 3
	wordConstructionHintStruggleCount   = 2
	wordConstructionWeaknessBoost       = 0.25
	wordConstructionDifficultyBoost     = 0.04
	forcedBuildWordMinReviewCount       = 4
	forcedBuildWordMinHardCount         = 2
	forcedBuildWordMinMode12Days        = 48 * time.Hour
	stuckRevealMinReviewCount           = 3
	stuckRevealMinHardCount             = 2
	stuckRevealMinMeaningRevealCount    = 2
	easyReviewIntervalMultiplier        = 1.65
	mediumReviewIntervalMultiplier      = 1.05
	hardReviewIntervalMultiplier        = 0.30
	minHardReviewInterval               = 4 * time.Hour
)

func ComputeWeakSlots(dailyLimit int) int {
	if dailyLimit <= 0 {
		return 0
	}
	slots := int(math.Ceil(float64(dailyLimit) / 3.0))
	if slots > 3 {
		return 3
	}
	return slots
}

func ComputeNewWordQuota(dailyLimit int, dueReview int, shortTerm int, weakSlots int) int {
	return ComputeNewWordPrefetchBatchSize(dailyLimit)
}

func ComputeNewWordPrefetchBatchSize(dailyLimit int) int {
	if dailyLimit < 0 {
		return 0
	}
	return dailyLimit * 2
}

func SelectReviewMode(state domain.UserWordState, memoryCauseBiasEnabled bool) domain.ReviewMode {
	var selected domain.ReviewMode
	switch state.LearningStage {
	case 1, 2:
		return domain.ReviewModeReveal
	case 3:
		if shouldUseAdvancedConstructionMode(state, memoryCauseBiasEnabled) {
			return selectAdvancedConstructionMode(state)
		}
		if memoryCauseBiasEnabled {
			switch state.LastMemoryCause {
			case domain.MemoryCauseSpellingIssue:
				selected = domain.ReviewModeBuildWord
			case domain.MemoryCauseMixedUpWord:
				selected = domain.ReviewModeMultipleChoice
			}
		}
		if selected == "" {
			if state.Difficulty >= transitionMode2DifficultyThreshold ||
				state.WeaknessScore >= transitionMode2WeaknessThreshold ||
				state.LastRating == domain.RatingHard {
				selected = alternatingMode2Reveal(state)
			} else {
				selected = enterWordConstructionMode(state)
			}
		}
		return maybeForceBuildWordMode(state, selected)
	default:
		if state.LearningStage > 0 {
			return domain.ReviewModeReveal
		}
	}

	if shouldUseAdvancedConstructionMode(state, memoryCauseBiasEnabled) {
		return selectAdvancedConstructionMode(state)
	}
	if memoryCauseBiasEnabled {
		switch state.LastMemoryCause {
		case domain.MemoryCauseForgotMeaning:
			selected = domain.ReviewModeReveal
		case domain.MemoryCauseMixedUpWord:
			selected = domain.ReviewModeMultipleChoice
		case domain.MemoryCauseSpellingIssue:
			selected = domain.ReviewModeBuildWord
		}
	}
	if selected == "" {
		if state.LastMode == domain.ReviewModeReveal &&
			state.LastRating == domain.RatingHard &&
			state.HardCount >= stuckRevealMinHardCount {
			selected = domain.ReviewModeMultipleChoice
		} else if state.WrongCount >= standardMode2WrongCountThreshold ||
			state.RevealMeaningCount >= standardMode2MeaningRevealThreshold {
			selected = domain.ReviewModeMultipleChoice
		} else if state.Difficulty >= standardMode2DifficultyThreshold ||
			state.WeaknessScore >= standardMode2WeaknessThreshold {
			selected = alternatingMode2Reveal(state)
		} else {
			selected = enterWordConstructionMode(state)
		}
	}
	return maybeForceBuildWordMode(state, selected)
}

func shouldUseAdvancedConstructionMode(state domain.UserWordState, memoryCauseBiasEnabled bool) bool {
	if state.WordConstructionSuccessStreak < advancedConstructionSuccessStreak {
		return false
	}
	if state.Difficulty >= advancedConstructionDifficultyMax || state.WeaknessScore >= advancedConstructionWeaknessMax {
		return false
	}
	if state.LastAnswerCorrect != nil && !*state.LastAnswerCorrect {
		return false
	}
	if memoryCauseBiasEnabled {
		switch state.LastMemoryCause {
		case domain.MemoryCauseForgotMeaning, domain.MemoryCauseMixedUpWord:
			return false
		}
	}
	return true
}

func selectAdvancedConstructionMode(state domain.UserWordState) domain.ReviewMode {
	seed := int(state.WordID[0]) +
		int(state.WordID[5]) +
		int(state.WordID[10]) +
		int(state.WordID[15]) +
		state.ReviewCount +
		state.WordConstructionSuccessStreak +
		state.WordConstructionStruggleCount
	if state.LastMode == domain.ReviewModeFillBlank {
		seed++
	}
	if seed%2 == 0 {
		return domain.ReviewModeFillBlank
	}
	return domain.ReviewModeListening
}

func SelectWordConstructionMode(state domain.UserWordState) domain.ReviewMode {
	seed := int(state.WordID[0]) +
		int(state.WordID[5]) +
		int(state.WordID[10]) +
		int(state.WordID[15]) +
		state.ReviewCount +
		state.WrongCount +
		state.HintUsedCount +
		state.SpellingIssueCount
	if state.LastSeenAt != nil {
		seed += int(state.LastSeenAt.Unix() / 60)
	}
	if state.LastMode == domain.ReviewModeBuildWord {
		seed++
	}
	if seed%2 == 0 {
		return domain.ReviewModeFillBlank
	}
	return domain.ReviewModeBuildWord
}

func enterWordConstructionMode(state domain.UserWordState) domain.ReviewMode {
	return domain.ReviewModeBuildWord
}

func alternatingMode2Reveal(state domain.UserWordState) domain.ReviewMode {
	if state.LastMode == domain.ReviewModeMultipleChoice {
		return domain.ReviewModeReveal
	}
	return domain.ReviewModeMultipleChoice
}

func maybeForceBuildWordMode(state domain.UserWordState, selected domain.ReviewMode) domain.ReviewMode {
	if selected == domain.ReviewModeReveal && shouldPromoteStuckRevealToMultipleChoice(state) {
		return domain.ReviewModeMultipleChoice
	}
	if selected != domain.ReviewModeReveal && selected != domain.ReviewModeMultipleChoice {
		return selected
	}
	if state.LastMode != domain.ReviewModeReveal && state.LastMode != domain.ReviewModeMultipleChoice {
		return selected
	}
	if !hasProlongedMode12Struggle(state) {
		return selected
	}
	return domain.ReviewModeBuildWord
}

func shouldPromoteStuckRevealToMultipleChoice(state domain.UserWordState) bool {
	if state.LastMode != domain.ReviewModeReveal {
		return false
	}
	if state.ReviewCount < stuckRevealMinReviewCount || state.HardCount < stuckRevealMinHardCount {
		return false
	}
	return state.LastRating == domain.RatingHard ||
		state.WrongCount >= stuckRevealMinHardCount ||
		state.RevealMeaningCount >= stuckRevealMinMeaningRevealCount ||
		state.MeaningForgetCount >= stuckRevealMinMeaningRevealCount
}

func hasProlongedMode12Struggle(state domain.UserWordState) bool {
	if state.ReviewCount < forcedBuildWordMinReviewCount || state.HardCount < forcedBuildWordMinHardCount {
		return false
	}
	if state.FirstSeenAt == nil || state.LastSeenAt == nil {
		return false
	}
	if state.LastSeenAt.Sub(*state.FirstSeenAt) < forcedBuildWordMinMode12Days {
		return false
	}
	return state.WrongCount >= 2 ||
		state.RevealMeaningCount >= 3 ||
		state.MeaningForgetCount >= 2 ||
		state.ConfusableMixupCount >= 2 ||
		state.SlowRecallCount >= 2 ||
		state.LastRating == domain.RatingHard
}

func UpdateAvgResponseTime(current int64, count int, value int) int64 {
	if value <= 0 {
		return current
	}
	if count <= 0 || current <= 0 {
		return int64(value)
	}
	return int64((float64(current*int64(count-1)) + float64(value)) / float64(count))
}

func ComputeWeaknessScore(state domain.UserWordState) float64 {
	return computeWeaknessScoreFromRatings(state)
}

func computeWeaknessScoreFromRatings(state domain.UserWordState) float64 {
	score := computeWeaknessSignal(state)
	recovery := computeWeaknessRecovery(state)
	return maxFloat(0, score-minFloat(recovery, score*0.75))
}

func computeWeaknessSignal(state domain.UserWordState) float64 {
	score := 0.0
	score += float64(state.HardCount) * 1.0
	score += float64(state.MediumCount) * 0.35
	score += float64(state.WordConstructionStruggleCount) * 0.18
	if state.HardCount >= 2 && state.MediumCount > 0 {
		score += minFloat(float64(state.MediumCount)*0.15, 0.6)
	}
	switch state.LastRating {
	case domain.RatingHard:
		score += 0.6
	case domain.RatingMedium:
		score += 0.2
	}
	return score
}

func computeWeaknessRecovery(state domain.UserWordState) float64 {
	recovery := float64(state.EasyCount) * 0.45
	recovery += float64(state.WordConstructionSuccessStreak) * 0.20
	switch state.LastRating {
	case domain.RatingEasy:
		recovery += 0.3
	}
	return recovery
}

func ApplyFirstExposureUnknown(state domain.UserWordState, now time.Time, responseTimeMs int) domain.UserWordState {
	state.Status = domain.WordStatusLearning
	state.FirstSeenAt = timePointerOrNow(state.FirstSeenAt, now)
	state.LastSeenAt = &now
	state.NextReviewAt = timePtr(now.Add(10 * time.Minute))
	state.IntervalSeconds = int((10 * time.Minute).Seconds())
	state.LearningStage = 1
	state.Stability = 0.5
	state.Difficulty = maxFloat(state.Difficulty, 0.5)
	state.ReviewCount++
	state.AvgResponseTimeMs = UpdateAvgResponseTime(state.AvgResponseTimeMs, state.ReviewCount, responseTimeMs)
	state.WeaknessScore = computeWeaknessScoreFromRatings(state)
	return state
}

func ApplyFirstExposureKnown(state domain.UserWordState, now time.Time, responseTimeMs int) domain.UserWordState {
	state.Status = domain.WordStatusKnown
	state.FirstSeenAt = timePointerOrNow(state.FirstSeenAt, now)
	state.LastSeenAt = &now
	state.KnownAt = &now
	state.NextReviewAt = nil
	state.IntervalSeconds = 0
	state.LearningStage = 0
	state.Stability = maxFloat(state.Stability, 3.0)
	state.Difficulty = minFloat(maxFloat(state.Difficulty-0.2, 0.1), 0.9)
	state.AvgResponseTimeMs = UpdateAvgResponseTime(state.AvgResponseTimeMs, maxInt(state.ReviewCount, 1), responseTimeMs)
	state.WeaknessScore = 0
	return state
}

func ApplyReviewOutcome(state domain.UserWordState, rating domain.ReviewRating, mode domain.ReviewMode, now time.Time, responseTimeMs int) domain.UserWordState {
	previousLastRating := state.LastRating
	previousReviewCount := state.ReviewCount
	previousEasyCount := state.EasyCount
	previousHardCount := state.HardCount

	state.LastSeenAt = &now
	state.LastRating = rating
	state.LastMode = mode
	state.ReviewCount++
	state.AvgResponseTimeMs = UpdateAvgResponseTime(state.AvgResponseTimeMs, state.ReviewCount, responseTimeMs)

	switch rating {
	case domain.RatingEasy:
		state.EasyCount++
	case domain.RatingMedium:
		state.MediumCount++
	case domain.RatingHard:
		state.HardCount++
		state.WrongCount++
	}

	if state.LearningStage > 0 {
		duration, nextStage, status := nextConsolidationStep(state.LearningStage, rating)
		state.LearningStage = nextStage
		state.Status = status
		state.IntervalSeconds = int(duration.Seconds())
		state.NextReviewAt = timePtr(now.Add(duration))
		if rating == domain.RatingHard {
			state.Difficulty = minFloat(state.Difficulty+0.12, 0.95)
			state.Stability = maxFloat(state.Stability*0.8, 0.4)
		} else {
			state.Difficulty = minFloat(maxFloat(state.Difficulty-0.05, 0.1), 0.95)
			state.Stability = maxFloat(state.Stability+0.3, 0.7)
		}
		state.WeaknessScore = computeWeaknessScoreFromRatings(state)
		return state
	}

	baseInterval := maxInt(state.IntervalSeconds, int((24 * time.Hour).Seconds()))
	multiplier := 1.0
	switch rating {
	case domain.RatingEasy:
		multiplier = effectiveEasyReviewIntervalMultiplier(state, previousEasyCount, previousHardCount)
		state.Difficulty = minFloat(maxFloat(state.Difficulty-0.08, 0.1), 0.95)
		state.Stability += 0.6
	case domain.RatingMedium:
		multiplier = effectiveMediumReviewIntervalMultiplier(state, previousReviewCount, previousEasyCount, previousHardCount, previousLastRating)
		state.Difficulty = minFloat(maxFloat(state.Difficulty-0.02, 0.1), 0.95)
		state.Stability += 0.25
	case domain.RatingHard:
		multiplier = hardReviewIntervalMultiplier
		state.Difficulty = minFloat(state.Difficulty+0.1, 0.95)
		state.Stability = maxFloat(state.Stability*0.85, 0.6)
	}
	seconds := int(float64(baseInterval) * multiplier * (1 + state.Stability/5))
	if rating == domain.RatingHard && seconds < int(minHardReviewInterval.Seconds()) {
		seconds = int(minHardReviewInterval.Seconds())
	}
	state.IntervalSeconds = seconds
	next := now.Add(time.Duration(seconds) * time.Second)
	state.NextReviewAt = &next
	state.Status = domain.WordStatusReview
	state.WeaknessScore = computeWeaknessScoreFromRatings(state)
	return state
}

func effectiveEasyReviewIntervalMultiplier(state domain.UserWordState, previousEasyCount int, previousHardCount int) float64 {
	multiplier := easyReviewIntervalMultiplier
	if previousHardCount == 0 && previousEasyCount >= 4 && state.WordConstructionStruggleCount == 0 {
		return 1.85
	}
	if previousHardCount >= 3 || state.WordConstructionStruggleCount >= 2 {
		multiplier = 1.45
	}
	return multiplier
}

func effectiveMediumReviewIntervalMultiplier(
	state domain.UserWordState,
	previousReviewCount int,
	previousEasyCount int,
	previousHardCount int,
	previousLastRating domain.ReviewRating,
) float64 {
	if previousLastRating == domain.RatingHard ||
		previousHardCount >= 2 ||
		state.WrongCount >= 3 ||
		state.WordConstructionStruggleCount >= 2 {
		return 0.82
	}
	if previousHardCount == 0 && previousEasyCount >= 3 && previousEasyCount*2 >= maxInt(previousReviewCount, 1) {
		return 1.25
	}
	return mediumReviewIntervalMultiplier
}

func ApplyBonusPracticeOutcome(state domain.UserWordState, rating domain.ReviewRating, mode domain.ReviewMode, now time.Time, responseTimeMs int) domain.UserWordState {
	state.LastSeenAt = &now
	state.LastRating = rating
	state.LastMode = mode

	baseline := state.WeaknessScore
	if baseline <= 0 {
		baseline = computeWeaknessScoreFromRatings(state)
	}

	switch rating {
	case domain.RatingEasy:
		multiplier := 0.55
		if responseTimeMs > 9000 {
			multiplier = 0.65
		}
		state.WeaknessScore = maxFloat(0, baseline*multiplier)
	case domain.RatingMedium:
		multiplier := 0.8
		if responseTimeMs > 9000 {
			multiplier = 0.9
		}
		state.WeaknessScore = maxFloat(0, baseline*multiplier)
	case domain.RatingHard:
		state.WrongCount++
		baseline = maxFloat(baseline, computeWeaknessScoreFromRatings(state))
		state.WeaknessScore = baseline + 0.35
	default:
		state.WeaknessScore = baseline
	}

	return state
}

func ApplyWordConstructionStruggle(state domain.UserWordState, mode domain.ReviewMode, answerCorrect bool, hintCount int, responseTimeMs int) domain.UserWordState {
	return ApplyWordConstructionFeedback(state, mode, answerCorrect, hintCount, wordConstructionDefaultHintLimit, 0, responseTimeMs, time.Time{})
}

func ApplyWordConstructionFeedback(
	state domain.UserWordState,
	mode domain.ReviewMode,
	answerCorrect bool,
	hintCount int,
	hintLimit int,
	wrongAttemptCount int,
	responseTimeMs int,
	now time.Time,
) domain.UserWordState {
	if !isWordConstructionMode(mode) {
		return state
	}
	hintThreshold := wordConstructionStruggleHintThreshold(mode, hintLimit)
	heavyHintUse := hintCount > hintThreshold
	wrongAttempts := wrongAttemptCount > 0
	cleanSuccess := answerCorrect &&
		!heavyHintUse &&
		!wrongAttempts &&
		(state.LastRating == domain.RatingEasy || state.LastRating == domain.RatingMedium)

	if cleanSuccess {
		state.WordConstructionSuccessStreak++
		return state
	}

	struggled := !answerCorrect ||
		state.LastRating == domain.RatingHard ||
		heavyHintUse ||
		wrongAttempts
	if !struggled {
		return state
	}
	state.WordConstructionSuccessStreak = 0
	state.WordConstructionStruggleCount++
	if state.LastMemoryCause != domain.MemoryCauseSpellingIssue {
		state.SpellingIssueCount++
	}
	state.LastMemoryCause = domain.MemoryCauseSpellingIssue
	state.LastResponseTimeMs = responseTimeMs
	state.LastAnswerCorrect = boolPointer(answerCorrect)
	difficultyBoost := wordConstructionDifficultyBoost + float64(minInt(wrongAttemptCount, 3))*0.02
	weaknessBoost := wordConstructionWeaknessBoost +
		float64(maxInt(hintCount-hintThreshold, 0))*0.08 +
		float64(minInt(wrongAttemptCount, 3))*0.10
	state.Difficulty = minFloat(state.Difficulty+difficultyBoost, 0.95)
	state.WeaknessScore = maxFloat(ComputeWeaknessScore(state), state.WeaknessScore+weaknessBoost)
	state = shortenReviewAfterConstructionStruggle(state, now, hintCount, hintThreshold, wrongAttemptCount, answerCorrect)
	return state
}

func isWordConstructionMode(mode domain.ReviewMode) bool {
	return mode == domain.ReviewModeBuildWord ||
		mode == domain.ReviewModeFillBlank ||
		mode == domain.ReviewModeListening
}

func wordConstructionStruggleHintThreshold(mode domain.ReviewMode, hintLimit int) int {
	if mode == domain.ReviewModeListening && hintLimit > 0 {
		return int(math.Round(float64(hintLimit) / 2.0))
	}
	return wordConstructionHintStruggleCount
}

func shortenReviewAfterConstructionStruggle(
	state domain.UserWordState,
	now time.Time,
	hintCount int,
	hintThreshold int,
	wrongAttemptCount int,
	answerCorrect bool,
) domain.UserWordState {
	if now.IsZero() || state.Status != domain.WordStatusReview || state.NextReviewAt == nil || state.IntervalSeconds <= 0 {
		return state
	}
	factor := 0.70
	if !answerCorrect || state.LastRating == domain.RatingHard {
		factor = 0.45
	} else if wrongAttemptCount > 0 {
		factor = 0.55
	} else if hintCount > hintThreshold {
		factor = 0.60
	}
	seconds := maxInt(int(minHardReviewInterval.Seconds()), int(float64(state.IntervalSeconds)*factor))
	if seconds >= state.IntervalSeconds {
		return state
	}
	state.IntervalSeconds = seconds
	next := now.Add(time.Duration(seconds) * time.Second)
	state.NextReviewAt = &next
	state.Stability = maxFloat(state.Stability*0.9, 0.5)
	return state
}

func nextConsolidationStep(stage int, rating domain.ReviewRating) (time.Duration, int, domain.WordStatus) {
	if rating == domain.RatingHard {
		switch stage {
		case 1:
			return 5 * time.Minute, 1, domain.WordStatusLearning
		case 2:
			return 8 * time.Hour, 2, domain.WordStatusLearning
		default:
			return 24 * time.Hour, 3, domain.WordStatusLearning
		}
	}

	switch stage {
	case 1:
		return 8 * time.Hour, 2, domain.WordStatusLearning
	case 2:
		return 24 * time.Hour, 3, domain.WordStatusLearning
	default:
		return 2 * 24 * time.Hour, 0, domain.WordStatusReview
	}
}

func timePointerOrNow(value *time.Time, now time.Time) *time.Time {
	if value != nil {
		return value
	}
	return &now
}

func timePtr(value time.Time) *time.Time { return &value }

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxFloat(a float64, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minFloat(a float64, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
