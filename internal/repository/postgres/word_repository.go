package postgres

import (
	"context"
	"fmt"
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
		       pronunciation_hint, vietnamese_meaning, english_meaning, example_sentence_1, example_sentence_2, common_rate, source_provider, source_metadata, created_at, updated_at
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
			          pronunciation_hint, vietnamese_meaning, english_meaning, example_sentence_1, example_sentence_2, common_rate, source_provider, source_metadata, created_at, updated_at
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
		          pronunciation_hint, vietnamese_meaning, english_meaning, example_sentence_1, example_sentence_2, common_rate, source_provider, source_metadata, created_at, updated_at
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
			       pronunciation_hint, vietnamese_meaning, english_meaning, example_sentence_1, example_sentence_2, common_rate, source_provider, source_metadata, created_at, updated_at
			FROM words w
			WHERE w.level = $2
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

func (r *WordRepository) ListWordsByIDs(ctx context.Context, ids []uuid.UUID) ([]domain.Word, error) {
	if len(ids) == 0 {
		return []domain.Word{}, nil
	}
	query := fmt.Sprintf(`
		SELECT id, word, normalized_form, canonical_form, lemma, word_family, confusable_group_key, part_of_speech, level, topic, ipa,
		       pronunciation_hint, vietnamese_meaning, english_meaning, example_sentence_1, example_sentence_2, common_rate, source_provider, source_metadata, created_at, updated_at
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
