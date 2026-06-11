package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"wordbit-advanced-app/backend/internal/domain"
)

const (
	statisticsRange7d  = "7d"
	statisticsRange30d = "30d"
	statisticsRange90d = "90d"
	statisticsRangeAll = "all"
)

var statisticsGlossary = map[string]string{
	"weakness_score":     "Diem nay cang cao thi tu do cang de bi quen hoac tra loi sai.",
	"stage_distribution": "Phan bo tu vung theo tung giai doan hoc hien tai cua ban.",
	"review_mode":        "Kieu cau hoi he thong da dung de kiem tra tu vung cua ban.",
	"current_streak":     "So ngay lien tiep gan day ban co hoan thanh it nhat mot hoat dong hoc.",
}

type StatisticsService struct {
	settingsRepo SettingsRepository
	wordRepo     WordRepository
	stateRepo    WordStateRepository
	eventRepo    LearningEventRepository
	clock        Clock
}

func NewStatisticsService(
	settingsRepo SettingsRepository,
	wordRepo WordRepository,
	stateRepo WordStateRepository,
	eventRepo LearningEventRepository,
	clock Clock,
) *StatisticsService {
	return &StatisticsService{
		settingsRepo: settingsRepo,
		wordRepo:     wordRepo,
		stateRepo:    stateRepo,
		eventRepo:    eventRepo,
		clock:        clock,
	}
}

func (s *StatisticsService) GetUserStatistics(ctx context.Context, userID uuid.UUID, requestedRange string) (domain.UserStatistics, error) {
	settings, err := s.settingsRepo.Get(ctx, userID)
	if err != nil {
		return domain.UserStatistics{}, err
	}
	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return domain.UserStatistics{}, err
	}
	normalizedRange, startUTC, endUTC, err := statisticsTimeWindow(s.clock.Now(), loc, requestedRange)
	if err != nil {
		return domain.UserStatistics{}, err
	}

	states, err := s.stateRepo.ListExistingWords(ctx, userID)
	if err != nil {
		return domain.UserStatistics{}, err
	}
	events, err := s.eventRepo.ListByUserTimeRange(ctx, userID, startUTC, endUTC)
	if err != nil {
		return domain.UserStatistics{}, err
	}

	activitySeries, dayStats, eventDayKeys := buildActivitySeries(events, loc, normalizedRange, startUTC.In(loc), endUTC.In(loc))
	stats := domain.UserStatistics{
		Range:                normalizedRange,
		Timezone:             settings.Timezone,
		Summary:              buildStatisticsSummary(states, dayStats, loc, s.clock.Now()),
		ActivitySeries:       activitySeries,
		StageDistribution:    buildStageDistribution(states),
		WeaknessDistribution: buildWeaknessDistribution(states),
		ModeDistribution:     buildModeDistribution(events),
		Glossary:             copyGlossary(statisticsGlossary),
	}

	if normalizedRange == statisticsRangeAll && len(eventDayKeys) == 0 {
		stats.ActivitySeries = []domain.StatisticsActivityPoint{}
	}

	topWords, err := buildTopDifficultWords(ctx, s.wordRepo, states)
	if err != nil {
		return domain.UserStatistics{}, err
	}
	stats.TopDifficultWords = topWords
	return stats, nil
}

func statisticsTimeWindow(now time.Time, loc *time.Location, requestedRange string) (string, time.Time, time.Time, error) {
	localNow := now.In(loc)
	endLocal := time.Date(localNow.Year(), localNow.Month(), localNow.Day()+1, 0, 0, 0, 0, loc)

	switch requestedRange {
	case "", statisticsRange7d:
		startLocal := endLocal.AddDate(0, 0, -7)
		return statisticsRange7d, startLocal.UTC(), endLocal.UTC(), nil
	case statisticsRange30d:
		startLocal := endLocal.AddDate(0, 0, -30)
		return statisticsRange30d, startLocal.UTC(), endLocal.UTC(), nil
	case statisticsRange90d:
		startLocal := endLocal.AddDate(0, 0, -90)
		return statisticsRange90d, startLocal.UTC(), endLocal.UTC(), nil
	case statisticsRangeAll:
		startLocal := time.Date(2000, 1, 1, 0, 0, 0, 0, loc)
		return statisticsRangeAll, startLocal.UTC(), endLocal.UTC(), nil
	default:
		return "", time.Time{}, time.Time{}, fmt.Errorf("%w: unsupported range", domain.ErrValidation)
	}
}

type statisticsDay struct {
	reviews  int
	newWords int
}

func buildActivitySeries(
	events []domain.LearningEvent,
	loc *time.Location,
	normalizedRange string,
	startLocal time.Time,
	endLocal time.Time,
) ([]domain.StatisticsActivityPoint, map[string]statisticsDay, []string) {
	byDay := make(map[string]statisticsDay)
	eventDayKeys := make([]string, 0)
	seenEventDays := make(map[string]struct{})

	for _, event := range events {
		dayKey := event.EventTime.In(loc).Format("2006-01-02")
		day := byDay[dayKey]
		switch event.EventType {
		case domain.EventTypeFirstExposure:
			day.newWords++
		case domain.EventTypeReviewAnswer, domain.EventTypeBonusPractice:
			day.reviews++
		}
		if day.newWords > 0 || day.reviews > 0 {
			byDay[dayKey] = day
			if _, ok := seenEventDays[dayKey]; !ok {
				seenEventDays[dayKey] = struct{}{}
				eventDayKeys = append(eventDayKeys, dayKey)
			}
		}
	}

	if normalizedRange == statisticsRangeAll && len(eventDayKeys) == 0 {
		return []domain.StatisticsActivityPoint{}, byDay, eventDayKeys
	}

	cursor := startLocal
	if normalizedRange == statisticsRangeAll && len(eventDayKeys) > 0 {
		sort.Strings(eventDayKeys)
		firstEventDay, _ := time.ParseInLocation("2006-01-02", eventDayKeys[0], loc)
		cursor = firstEventDay
	}

	series := make([]domain.StatisticsActivityPoint, 0)
	for cursor.Before(endLocal) {
		dayKey := cursor.Format("2006-01-02")
		day := byDay[dayKey]
		series = append(series, domain.StatisticsActivityPoint{
			Date:         dayKey,
			Label:        cursor.Format("02/01"),
			ReviewCount:  day.reviews,
			NewWordCount: day.newWords,
		})
		cursor = cursor.AddDate(0, 0, 1)
	}
	return series, byDay, eventDayKeys
}

func buildStatisticsSummary(states []domain.UserWordState, dayStats map[string]statisticsDay, loc *time.Location, now time.Time) domain.StatisticsSummary {
	summary := domain.StatisticsSummary{}
	for _, state := range states {
		summary.TotalLearnedWords++
		if state.Status == domain.WordStatusLearning || state.Status == domain.WordStatusReview {
			summary.ActiveReviewWords++
		}
	}
	for _, day := range dayStats {
		summary.ReviewCount += day.reviews
		summary.NewWordCount += day.newWords
	}
	summary.CurrentStreakDays = currentStreakDays(dayStats, loc, now)
	return summary
}

func currentStreakDays(dayStats map[string]statisticsDay, loc *time.Location, now time.Time) int {
	localNow := now.In(loc)
	cursor := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	streak := 0
	for {
		dayKey := cursor.Format("2006-01-02")
		day, ok := dayStats[dayKey]
		if !ok || (day.reviews == 0 && day.newWords == 0) {
			break
		}
		streak++
		cursor = cursor.AddDate(0, 0, -1)
	}
	return streak
}

func buildStageDistribution(states []domain.UserWordState) []domain.StatisticsBreakdownItem {
	counts := map[int]int{}
	for _, state := range states {
		counts[state.LearningStage]++
	}
	keys := make([]int, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	out := make([]domain.StatisticsBreakdownItem, 0, len(keys))
	for _, key := range keys {
		out = append(out, domain.StatisticsBreakdownItem{
			Key:   fmt.Sprintf("stage_%d", key),
			Label: fmt.Sprintf("Stage %d", key),
			Count: counts[key],
		})
	}
	return out
}

func buildWeaknessDistribution(states []domain.UserWordState) []domain.StatisticsBreakdownItem {
	type bucket struct {
		key   string
		label string
		count int
	}
	buckets := []bucket{
		{key: "steady", label: "On track"},
		{key: "watch", label: "Need attention"},
		{key: "fragile", label: "Fragile memory"},
		{key: "critical", label: "Very weak"},
	}
	for _, state := range states {
		switch {
		case state.WeaknessScore < 0.75:
			buckets[0].count++
		case state.WeaknessScore < 1.5:
			buckets[1].count++
		case state.WeaknessScore < 2.5:
			buckets[2].count++
		default:
			buckets[3].count++
		}
	}
	out := make([]domain.StatisticsBreakdownItem, 0, len(buckets))
	for _, bucket := range buckets {
		out = append(out, domain.StatisticsBreakdownItem{
			Key:   bucket.key,
			Label: bucket.label,
			Count: bucket.count,
		})
	}
	return out
}

func buildModeDistribution(events []domain.LearningEvent) []domain.StatisticsBreakdownItem {
	order := []struct {
		key   string
		label string
	}{
		{key: string(domain.ReviewModeReveal), label: "Hidden meaning"},
		{key: string(domain.ReviewModeMultipleChoice), label: "Multiple choice"},
		{key: string(domain.ReviewModeBuildWord), label: "Build word"},
		{key: string(domain.ReviewModeFillBlank), label: "Fill in blank"},
		{key: string(domain.ReviewModeListening), label: "Listening sentence"},
	}
	counts := map[string]int{}
	for _, event := range events {
		switch event.EventType {
		case domain.EventTypeReviewAnswer, domain.EventTypeBonusPractice:
			if event.ModeUsed != "" {
				counts[string(event.ModeUsed)]++
			}
		}
	}
	out := make([]domain.StatisticsBreakdownItem, 0, len(order))
	for _, item := range order {
		out = append(out, domain.StatisticsBreakdownItem{
			Key:   item.key,
			Label: item.label,
			Count: counts[item.key],
		})
	}
	return out
}

func buildTopDifficultWords(ctx context.Context, wordRepo WordRepository, states []domain.UserWordState) ([]domain.StatisticsTopWord, error) {
	if len(states) == 0 {
		return []domain.StatisticsTopWord{}, nil
	}
	sortedStates := append([]domain.UserWordState(nil), states...)
	sort.Slice(sortedStates, func(i, j int) bool {
		if sortedStates[i].WeaknessScore == sortedStates[j].WeaknessScore {
			return sortedStates[i].ReviewCount > sortedStates[j].ReviewCount
		}
		return sortedStates[i].WeaknessScore > sortedStates[j].WeaknessScore
	})
	if len(sortedStates) > 5 {
		sortedStates = sortedStates[:5]
	}
	wordIDs := make([]uuid.UUID, 0, len(sortedStates))
	for _, state := range sortedStates {
		wordIDs = append(wordIDs, state.WordID)
	}
	words, err := wordRepo.ListWordsByIDs(ctx, wordIDs)
	if err != nil {
		return nil, err
	}
	wordByID := make(map[uuid.UUID]domain.Word, len(words))
	for _, word := range words {
		wordByID[word.ID] = word
	}
	out := make([]domain.StatisticsTopWord, 0, len(sortedStates))
	for _, state := range sortedStates {
		word := wordByID[state.WordID]
		out = append(out, domain.StatisticsTopWord{
			Word:              word.Word,
			WordID:            state.WordID,
			VietnameseMeaning: word.VietnameseMeaning,
			EnglishMeaning:    word.EnglishMeaning,
			WeaknessScore:     state.WeaknessScore,
			ReviewCount:       state.ReviewCount,
			LearningStage:     state.LearningStage,
		})
	}
	return out, nil
}

func copyGlossary(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}
