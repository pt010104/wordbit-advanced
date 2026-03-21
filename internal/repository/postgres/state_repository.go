package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"wordbit-advanced-app/backend/internal/domain"
)

type WordStateRepository struct {
	pool *pgxpool.Pool
}

func (r *WordStateRepository) Get(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) (domain.UserWordState, error) {
	return scanState(r.pool.QueryRow(ctx, `
		SELECT user_id, word_id, status, first_seen_at, last_seen_at, last_rating, next_review_at, interval_seconds, stability, difficulty,
		       review_count, wrong_count, easy_count, medium_count, hard_count, hint_used_count, reveal_meaning_count, reveal_example_count,
		       avg_response_time_ms, weakness_score, learning_stage, last_mode, last_memory_cause, last_response_time_ms, last_answer_correct,
		       meaning_forget_count, spelling_issue_count, confusable_mixup_count, slow_recall_count, guessed_correct_count,
		       known_at, created_at, updated_at
		FROM user_word_states
		WHERE user_id = $1 AND word_id = $2
	`, userID, wordID))
}

func (r *WordStateRepository) ListDueWithinWindow(ctx context.Context, userID uuid.UUID, start time.Time, end time.Time, learningOnly bool) ([]domain.UserWordState, error) {
	where := "status = 'review' AND learning_stage = 0"
	if learningOnly {
		where = "status = 'learning' AND learning_stage > 0"
	}
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT user_id, word_id, status, first_seen_at, last_seen_at, last_rating, next_review_at, interval_seconds, stability, difficulty,
		       review_count, wrong_count, easy_count, medium_count, hard_count, hint_used_count, reveal_meaning_count, reveal_example_count,
		       avg_response_time_ms, weakness_score, learning_stage, last_mode, last_memory_cause, last_response_time_ms, last_answer_correct,
		       meaning_forget_count, spelling_issue_count, confusable_mixup_count, slow_recall_count, guessed_correct_count,
		       known_at, created_at, updated_at
		FROM user_word_states
		WHERE user_id = $1
		  AND %s
		  AND next_review_at >= $2
		  AND next_review_at <= $3
		ORDER BY next_review_at ASC
	`, where), userID, start, end)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var states []domain.UserWordState
	for rows.Next() {
		state, scanErr := scanState(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		states = append(states, state)
	}
	return states, rows.Err()
}

func (r *WordStateRepository) ListWeakCandidates(ctx context.Context, userID uuid.UUID, excludeWordIDs []uuid.UUID, limit int) ([]domain.UserWordState, error) {
	if limit <= 0 {
		return []domain.UserWordState{}, nil
	}
	query := `
		SELECT user_id, word_id, status, first_seen_at, last_seen_at, last_rating, next_review_at, interval_seconds, stability, difficulty,
		       review_count, wrong_count, easy_count, medium_count, hard_count, hint_used_count, reveal_meaning_count, reveal_example_count,
		       avg_response_time_ms, weakness_score, learning_stage, last_mode, last_memory_cause, last_response_time_ms, last_answer_correct,
		       meaning_forget_count, spelling_issue_count, confusable_mixup_count, slow_recall_count, guessed_correct_count,
		       known_at, created_at, updated_at
		FROM user_word_states
		WHERE user_id = $1
		  AND status IN ('learning', 'review')
	`
	args := []any{userID}
	if len(excludeWordIDs) > 0 {
		query += fmt.Sprintf(" AND word_id NOT IN (%s)", joinPlaceholders(2, len(excludeWordIDs)))
		args = append(args, inClauseUUIDs(excludeWordIDs)...)
	}
	query += fmt.Sprintf(" ORDER BY weakness_score DESC, COALESCE(last_seen_at, created_at) ASC LIMIT $%d", len(args)+1)
	args = append(args, limit)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var states []domain.UserWordState
	for rows.Next() {
		state, scanErr := scanState(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		states = append(states, state)
	}
	return states, rows.Err()
}

func (r *WordStateRepository) ListExistingWords(ctx context.Context, userID uuid.UUID) ([]domain.UserWordState, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT user_id, word_id, status, first_seen_at, last_seen_at, last_rating, next_review_at, interval_seconds, stability, difficulty,
		       review_count, wrong_count, easy_count, medium_count, hard_count, hint_used_count, reveal_meaning_count, reveal_example_count,
		       avg_response_time_ms, weakness_score, learning_stage, last_mode, last_memory_cause, last_response_time_ms, last_answer_correct,
		       meaning_forget_count, spelling_issue_count, confusable_mixup_count, slow_recall_count, guessed_correct_count,
		       known_at, created_at, updated_at
		FROM user_word_states
		WHERE user_id = $1
	`, userID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var states []domain.UserWordState
	for rows.Next() {
		state, scanErr := scanState(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		states = append(states, state)
	}
	return states, rows.Err()
}

func (r *WordStateRepository) ListDictionaryEntries(ctx context.Context, userID uuid.UUID, filter domain.DictionaryFilter, query string, limit int, offset int) ([]domain.DictionaryEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	baseQuery := `
			SELECT w.id, w.word, w.normalized_form, w.canonical_form, w.lemma, w.word_family, w.confusable_group_key, w.part_of_speech, w.level, w.topic,
			       w.ipa, w.pronunciation_hint, w.vietnamese_meaning, w.english_meaning, w.example_sentence_1, w.example_sentence_2, w.common_rate, w.source_provider, w.source_metadata,
			       w.created_at, w.updated_at,
			       CASE WHEN s.status = 'known' THEN 'known' ELSE 'unknown' END AS list_status,
			       s.status, s.first_seen_at, s.last_seen_at, s.known_at, s.next_review_at, s.review_count, s.weakness_score, s.updated_at
		FROM user_word_states s
		JOIN words w ON w.id = s.word_id
		WHERE s.user_id = $1
	`
	args := []any{userID}

	switch filter {
	case domain.DictionaryFilterKnown:
		baseQuery += " AND s.status = 'known'"
	case domain.DictionaryFilterUnknown:
		baseQuery += " AND s.status IN ('learning', 'review')"
	}

	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery != "" {
		pattern := "%" + trimmedQuery + "%"
		baseQuery += fmt.Sprintf(" AND (w.word ILIKE $%d OR w.lemma ILIKE $%d OR w.english_meaning ILIKE $%d OR w.vietnamese_meaning ILIKE $%d)", len(args)+1, len(args)+1, len(args)+1, len(args)+1)
		args = append(args, pattern)
	}

	baseQuery += fmt.Sprintf(`
		ORDER BY
			CASE WHEN s.status = 'known' THEN COALESCE(s.known_at, s.updated_at) ELSE COALESCE(s.next_review_at, s.updated_at) END DESC NULLS LAST,
			LOWER(w.word) ASC
		LIMIT $%d OFFSET $%d
	`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, baseQuery, args...)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var entries []domain.DictionaryEntry
	for rows.Next() {
		entry, scanErr := scanDictionaryEntry(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (r *WordStateRepository) Upsert(ctx context.Context, state domain.UserWordState) (domain.UserWordState, error) {
	query := `
		INSERT INTO user_word_states (
			user_id, word_id, status, first_seen_at, last_seen_at, last_rating, next_review_at, interval_seconds,
			stability, difficulty, review_count, wrong_count, easy_count, medium_count, hard_count, hint_used_count,
			reveal_meaning_count, reveal_example_count, avg_response_time_ms, weakness_score, learning_stage, last_mode, last_memory_cause,
			last_response_time_ms, last_answer_correct, meaning_forget_count, spelling_issue_count, confusable_mixup_count,
			slow_recall_count, guessed_correct_count, known_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13, $14, $15, $16,
			$17, $18, $19, $20, $21, $22, $23,
			$24, $25, $26, $27, $28, $29,
			$30, $31
		)
		ON CONFLICT (user_id, word_id) DO UPDATE SET
			status = EXCLUDED.status,
			first_seen_at = EXCLUDED.first_seen_at,
			last_seen_at = EXCLUDED.last_seen_at,
			last_rating = EXCLUDED.last_rating,
			next_review_at = EXCLUDED.next_review_at,
			interval_seconds = EXCLUDED.interval_seconds,
			stability = EXCLUDED.stability,
			difficulty = EXCLUDED.difficulty,
			review_count = EXCLUDED.review_count,
			wrong_count = EXCLUDED.wrong_count,
			easy_count = EXCLUDED.easy_count,
			medium_count = EXCLUDED.medium_count,
			hard_count = EXCLUDED.hard_count,
			hint_used_count = EXCLUDED.hint_used_count,
			reveal_meaning_count = EXCLUDED.reveal_meaning_count,
			reveal_example_count = EXCLUDED.reveal_example_count,
			avg_response_time_ms = EXCLUDED.avg_response_time_ms,
			weakness_score = EXCLUDED.weakness_score,
			learning_stage = EXCLUDED.learning_stage,
			last_mode = EXCLUDED.last_mode,
			last_memory_cause = EXCLUDED.last_memory_cause,
			last_response_time_ms = EXCLUDED.last_response_time_ms,
			last_answer_correct = EXCLUDED.last_answer_correct,
			meaning_forget_count = EXCLUDED.meaning_forget_count,
			spelling_issue_count = EXCLUDED.spelling_issue_count,
			confusable_mixup_count = EXCLUDED.confusable_mixup_count,
			slow_recall_count = EXCLUDED.slow_recall_count,
			guessed_correct_count = EXCLUDED.guessed_correct_count,
			known_at = EXCLUDED.known_at
		RETURNING user_id, word_id, status, first_seen_at, last_seen_at, last_rating, next_review_at, interval_seconds, stability, difficulty,
		          review_count, wrong_count, easy_count, medium_count, hard_count, hint_used_count, reveal_meaning_count, reveal_example_count,
		          avg_response_time_ms, weakness_score, learning_stage, last_mode, last_memory_cause, last_response_time_ms, last_answer_correct,
		          meaning_forget_count, spelling_issue_count, confusable_mixup_count, slow_recall_count, guessed_correct_count,
		          known_at, created_at, updated_at
	`
	return scanState(r.pool.QueryRow(ctx, query,
		state.UserID,
		state.WordID,
		state.Status,
		state.FirstSeenAt,
		state.LastSeenAt,
		string(state.LastRating),
		state.NextReviewAt,
		state.IntervalSeconds,
		state.Stability,
		state.Difficulty,
		state.ReviewCount,
		state.WrongCount,
		state.EasyCount,
		state.MediumCount,
		state.HardCount,
		state.HintUsedCount,
		state.RevealMeaningCount,
		state.RevealExampleCount,
		state.AvgResponseTimeMs,
		state.WeaknessScore,
		state.LearningStage,
		string(state.LastMode),
		nullableMemoryCauseValue(state.LastMemoryCause),
		state.LastResponseTimeMs,
		nullableBoolValue(state.LastAnswerCorrect),
		state.MeaningForgetCount,
		state.SpellingIssueCount,
		state.ConfusableMixupCount,
		state.SlowRecallCount,
		state.GuessedCorrectCount,
		state.KnownAt,
	))
}

func (r *WordStateRepository) Delete(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM user_word_states
		WHERE user_id = $1 AND word_id = $2
	`, userID, wordID)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *WordStateRepository) RefreshWeaknessScores(ctx context.Context, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE user_word_states
		SET weakness_score = (
			(wrong_count * 0.8) +
			(hint_used_count * 0.5) +
			(reveal_meaning_count * 0.3) +
			(reveal_example_count * 0.2) +
			(CASE WHEN avg_response_time_ms > 7000 THEN 0.6 ELSE 0 END) +
			(CASE WHEN stability < 1.0 THEN 0.4 ELSE 0 END)
		)
		WHERE user_id = $1
	`, userID)
	return mapError(err)
}
