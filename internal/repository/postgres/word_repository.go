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

type WordRepository struct {
	pool *pgxpool.Pool
}

func (r *WordRepository) GetByID(ctx context.Context, wordID uuid.UUID) (domain.Word, error) {
	return scanWord(r.pool.QueryRow(ctx, `
		SELECT id, word, normalized_form, canonical_form, lemma, word_family, confusable_group_key, part_of_speech, level, topic, ipa,
		       pronunciation_hint, vietnamese_meaning, english_meaning, example_sentence_1, example_sentence_2, generated_examples, common_rate, source_provider, source_metadata, created_at, updated_at
		FROM words
		WHERE id = $1
	`, wordID))
}

func (r *WordRepository) UpsertWord(ctx context.Context, candidate domain.CandidateWord) (domain.Word, error) {
	if candidate.NormalizedForm == "" {
		candidate.NormalizedForm = candidate.Word
	}
	query := `
			INSERT INTO words (
				word, normalized_form, canonical_form, lemma, word_family, confusable_group_key, part_of_speech, level,
				topic, ipa, pronunciation_hint, vietnamese_meaning, english_meaning, example_sentence_1, example_sentence_2,
				common_rate, source_provider, source_metadata
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8,
				$9, $10, $11, $12, $13, $14, $15,
				$16, $17, $18::jsonb
			)
			ON CONFLICT (normalized_form, part_of_speech) DO UPDATE SET
				word = EXCLUDED.word,
			canonical_form = EXCLUDED.canonical_form,
			lemma = EXCLUDED.lemma,
			word_family = EXCLUDED.word_family,
			confusable_group_key = EXCLUDED.confusable_group_key,
			level = EXCLUDED.level,
			topic = EXCLUDED.topic,
			ipa = EXCLUDED.ipa,
			pronunciation_hint = EXCLUDED.pronunciation_hint,
				vietnamese_meaning = EXCLUDED.vietnamese_meaning,
				english_meaning = EXCLUDED.english_meaning,
				example_sentence_1 = EXCLUDED.example_sentence_1,
				example_sentence_2 = EXCLUDED.example_sentence_2,
				common_rate = COALESCE(EXCLUDED.common_rate, words.common_rate),
				source_provider = EXCLUDED.source_provider,
				source_metadata = EXCLUDED.source_metadata
			RETURNING id, word, normalized_form, canonical_form, lemma, word_family, confusable_group_key, part_of_speech, level, topic, ipa,
			          pronunciation_hint, vietnamese_meaning, english_meaning, example_sentence_1, example_sentence_2, generated_examples, common_rate, source_provider, source_metadata, created_at, updated_at
	`
	return scanWord(r.pool.QueryRow(ctx, query,
		candidate.Word,
		candidate.NormalizedForm,
		candidate.CanonicalForm,
		candidate.Lemma,
		candidate.WordFamily,
		candidate.ConfusableGroupKey,
		candidate.PartOfSpeech,
		candidate.Level,
		candidate.Topic,
		candidate.IPA,
		candidate.PronunciationHint,
		candidate.VietnameseMeaning,
		candidate.EnglishMeaning,
		candidate.ExampleSentence1,
		candidate.ExampleSentence2,
		nullableCommonRateValue(candidate.CommonRate),
		candidate.SourceProvider,
		fromJSONMap(candidate.SourceMetadata),
	))
}

// UpdateDeveloperWordImportantScore applies one global learning-priority score
// to every curated card with the same normalized spelling, regardless of POS.
func (r *WordRepository) UpdateDeveloperWordImportantScore(ctx context.Context, normalizedForm string, score float64) (int64, error) {
	result, err := r.pool.Exec(ctx, `
		UPDATE words
		SET source_metadata = jsonb_set(
			COALESCE(source_metadata, '{}'::jsonb),
			'{important_score}',
			to_jsonb($2::numeric),
			true
		), updated_at = NOW()
		WHERE source_provider = 'developer_list'
		  AND normalized_form = $1
	`, normalizedForm, score)
	if err != nil {
		return 0, mapError(err)
	}
	return result.RowsAffected(), nil
}

func (r *WordRepository) UpdateWord(ctx context.Context, wordID uuid.UUID, candidate domain.CandidateWord) (domain.Word, error) {
	if candidate.NormalizedForm == "" {
		candidate.NormalizedForm = candidate.Word
	}
	query := `
		UPDATE words
		SET word = $2,
		    normalized_form = $3,
		    canonical_form = $4,
		    lemma = $5,
		    word_family = $6,
		    confusable_group_key = $7,
		    part_of_speech = $8,
		    level = $9,
		    topic = $10,
		    ipa = $11,
		    pronunciation_hint = $12,
		    vietnamese_meaning = $13,
		    english_meaning = $14,
		    example_sentence_1 = $15,
		    example_sentence_2 = $16,
		    common_rate = COALESCE($17, common_rate),
		    source_provider = $18,
		    source_metadata = $19::jsonb
		WHERE id = $1
		RETURNING id, word, normalized_form, canonical_form, lemma, word_family, confusable_group_key, part_of_speech, level, topic, ipa,
		          pronunciation_hint, vietnamese_meaning, english_meaning, example_sentence_1, example_sentence_2, generated_examples, common_rate, source_provider, source_metadata, created_at, updated_at
	`
	return scanWord(r.pool.QueryRow(ctx, query,
		wordID,
		candidate.Word,
		candidate.NormalizedForm,
		candidate.CanonicalForm,
		candidate.Lemma,
		candidate.WordFamily,
		candidate.ConfusableGroupKey,
		candidate.PartOfSpeech,
		candidate.Level,
		candidate.Topic,
		candidate.IPA,
		candidate.PronunciationHint,
		candidate.VietnameseMeaning,
		candidate.EnglishMeaning,
		candidate.ExampleSentence1,
		candidate.ExampleSentence2,
		nullableCommonRateValue(candidate.CommonRate),
		candidate.SourceProvider,
		fromJSONMap(candidate.SourceMetadata),
	))
}

// AppendGeneratedExamples merges the supplied example sentences into the word's
// generated_examples list, skipping blanks and entries already present (in either
// the curated example_sentence_1/2 columns or generated_examples), capping the
// stored list at maxGeneratedExamples (oldest entries are dropped first). It
// returns the resulting list.
func (r *WordRepository) AppendGeneratedExamples(ctx context.Context, wordID uuid.UUID, examples []string, maxGeneratedExamples int) ([]string, error) {
	word, err := r.GetByID(ctx, wordID)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	addSeen := func(s string) {
		key := strings.ToLower(strings.TrimSpace(s))
		if key != "" {
			seen[key] = struct{}{}
		}
	}
	addSeen(word.ExampleSentence1)
	addSeen(word.ExampleSentence2)

	merged := make([]string, 0, len(word.GeneratedExamples)+len(examples))
	for _, existing := range word.GeneratedExamples {
		key := strings.ToLower(strings.TrimSpace(existing))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, existing)
	}

	changed := false
	for _, candidate := range examples {
		trimmed := strings.TrimSpace(candidate)
		key := strings.ToLower(trimmed)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, trimmed)
		changed = true
	}

	if !changed {
		return word.GeneratedExamples, nil
	}

	if maxGeneratedExamples > 0 && len(merged) > maxGeneratedExamples {
		merged = merged[len(merged)-maxGeneratedExamples:]
	}

	if _, err := r.pool.Exec(ctx, `
		UPDATE words
		SET generated_examples = $2::jsonb,
		    updated_at = now()
		WHERE id = $1
	`, wordID, fromStringSlice(merged)); err != nil {
		return nil, mapError(err)
	}
	return merged, nil
}

func (r *WordRepository) ListWordIDsSeenAsNew(ctx context.Context, userID uuid.UUID, since time.Time) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT word_id
		FROM daily_learning_pool_items
		WHERE user_id = $1
		  AND item_type = 'new'
		  AND created_at >= $2
	`, userID, since)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, mapError(err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *WordRepository) ListBankWords(ctx context.Context, userID uuid.UUID, level domain.CEFRLevel, topic string, excludeWordIDs []uuid.UUID, limit int) ([]domain.Word, error) {
	if limit <= 0 {
		return []domain.Word{}, nil
	}
	query := `
			SELECT id, word, normalized_form, canonical_form, lemma, word_family, confusable_group_key, part_of_speech, level, topic, ipa,
			       pronunciation_hint, vietnamese_meaning, english_meaning, example_sentence_1, example_sentence_2, generated_examples, common_rate, source_provider, source_metadata, created_at, updated_at
			FROM words w
			WHERE w.level = $2
		  AND w.topic = $3
		  AND COALESCE(w.source_provider, '') NOT IN ('developer_list', 'developer_list_llm_fallback')
		  AND NOT EXISTS (
			SELECT 1
			FROM user_word_states s
			WHERE s.user_id = $1
			  AND s.word_id = w.id
		  )
	`
	args := []any{userID, level, topic}
	if len(excludeWordIDs) > 0 {
		query += fmt.Sprintf(" AND w.id NOT IN (%s)", joinPlaceholders(4, len(excludeWordIDs)))
		args = append(args, inClauseUUIDs(excludeWordIDs)...)
	}
	query += fmt.Sprintf(" ORDER BY w.created_at ASC LIMIT $%d", len(args)+1)
	args = append(args, limit)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var words []domain.Word
	for rows.Next() {
		word, scanErr := scanWord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		words = append(words, word)
	}
	return words, rows.Err()
}

// ListDeveloperListWords returns only developer-curated entries. Keeping this
// source separate from the reusable LLM bank ensures developer-list mode can
// never silently fall back to generated words.
func (r *WordRepository) ListDeveloperListWords(ctx context.Context, userID uuid.UUID, level domain.CEFRLevel, topic string, excludeWordIDs []uuid.UUID, limit int) ([]domain.Word, error) {
	if limit <= 0 {
		return []domain.Word{}, nil
	}
	query := `
		SELECT id, word, normalized_form, canonical_form, lemma, word_family, confusable_group_key, part_of_speech, level, topic, ipa,
		       pronunciation_hint, vietnamese_meaning, english_meaning, example_sentence_1, example_sentence_2, generated_examples, common_rate, source_provider, source_metadata, created_at, updated_at
		FROM words w
		WHERE w.source_provider IN ('developer_list', 'developer_list_llm_fallback')
		  AND w.level = $2
		  AND w.topic = $3
		  AND NOT EXISTS (
			SELECT 1
			FROM user_word_states s
			WHERE s.user_id = $1
			  AND s.word_id = w.id
		  )
	`
	args := []any{userID, level, topic}
	if len(excludeWordIDs) > 0 {
		query += fmt.Sprintf(" AND w.id NOT IN (%s)", joinPlaceholders(4, len(excludeWordIDs)))
		args = append(args, inClauseUUIDs(excludeWordIDs)...)
	}
	query += fmt.Sprintf(" ORDER BY COALESCE((w.source_metadata ->> 'important_score')::numeric, 0) DESC, CASE w.source_provider WHEN 'developer_list' THEN 0 ELSE 1 END, COALESCE((w.source_metadata ->> 'sort_order')::integer, 2147483647), w.created_at ASC LIMIT $%d", len(args)+1)
	args = append(args, limit)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	words := make([]domain.Word, 0, limit)
	for rows.Next() {
		word, scanErr := scanWord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		words = append(words, word)
	}
	return words, rows.Err()
}

func (r *WordRepository) ListWordsByIDs(ctx context.Context, ids []uuid.UUID) ([]domain.Word, error) {
	if len(ids) == 0 {
		return []domain.Word{}, nil
	}
	query := fmt.Sprintf(`
		SELECT id, word, normalized_form, canonical_form, lemma, word_family, confusable_group_key, part_of_speech, level, topic, ipa,
		       pronunciation_hint, vietnamese_meaning, english_meaning, example_sentence_1, example_sentence_2, generated_examples, common_rate, source_provider, source_metadata, created_at, updated_at
		FROM words
		WHERE id IN (%s)
	`, joinPlaceholders(1, len(ids)))
	rows, err := r.pool.Query(ctx, query, inClauseUUIDs(ids)...)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var words []domain.Word
	for rows.Next() {
		word, scanErr := scanWord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		words = append(words, word)
	}
	return words, rows.Err()
}
