package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"wordbit-advanced-app/backend/internal/domain"
)

type Mode4ReviewRepository struct {
	pool *pgxpool.Pool
}

const mode4PassageSelect = `
	SELECT id, user_id, generation_number, word_ids_json, source_words_json, plain_passage_text, marked_passage_markdown,
	       passage_spans_json, status, skip_count, last_skipped_at, completed_at, llm_run_id, created_at, updated_at
	FROM mode4_review_passages
`

func (r *Mode4ReviewRepository) GetOrCreateState(ctx context.Context, userID uuid.UUID) (domain.Mode4ReviewState, error) {
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO mode4_review_states (user_id)
		VALUES ($1)
		ON CONFLICT (user_id) DO NOTHING
	`, userID); err != nil {
		return domain.Mode4ReviewState{}, mapError(err)
	}
	return scanMode4ReviewState(r.pool.QueryRow(ctx, `
		SELECT user_id, generation_count, active_passage_id, last_completed_at, next_eligible_at, created_at, updated_at
		FROM mode4_review_states
		WHERE user_id = $1
	`, userID))
}

func (r *Mode4ReviewRepository) UpsertState(ctx context.Context, state domain.Mode4ReviewState) (domain.Mode4ReviewState, error) {
	return scanMode4ReviewState(r.pool.QueryRow(ctx, `
		INSERT INTO mode4_review_states (
			user_id, generation_count, active_passage_id, last_completed_at, next_eligible_at
		) VALUES (
			$1, $2, $3, $4, $5
		)
		ON CONFLICT (user_id) DO UPDATE SET
			generation_count = EXCLUDED.generation_count,
			active_passage_id = EXCLUDED.active_passage_id,
			last_completed_at = EXCLUDED.last_completed_at,
			next_eligible_at = EXCLUDED.next_eligible_at
		RETURNING user_id, generation_count, active_passage_id, last_completed_at, next_eligible_at, created_at, updated_at
	`,
		state.UserID,
		state.GenerationCount,
		state.ActivePassageID,
		state.LastCompletedAt,
		state.NextEligibleAt,
	))
}

func (r *Mode4ReviewRepository) GetActivePassage(ctx context.Context, userID uuid.UUID) (domain.Mode4ReviewPassage, error) {
	return scanMode4ReviewPassage(r.pool.QueryRow(ctx, mode4PassageSelect+`
		WHERE user_id = $1
		  AND status = 'active'
		ORDER BY generation_number DESC
		LIMIT 1
	`, userID))
}

func (r *Mode4ReviewRepository) GetPassage(ctx context.Context, userID uuid.UUID, passageID uuid.UUID) (domain.Mode4ReviewPassage, error) {
	return scanMode4ReviewPassage(r.pool.QueryRow(ctx, mode4PassageSelect+`
		WHERE user_id = $1
		  AND id = $2
	`, userID, passageID))
}

func (r *Mode4ReviewRepository) GetPassageByGeneration(ctx context.Context, userID uuid.UUID, generationNumber int) (domain.Mode4ReviewPassage, error) {
	return scanMode4ReviewPassage(r.pool.QueryRow(ctx, mode4PassageSelect+`
		WHERE user_id = $1
		  AND generation_number = $2
	`, userID, generationNumber))
}

func (r *Mode4ReviewRepository) GetLatestPassage(ctx context.Context, userID uuid.UUID) (domain.Mode4ReviewPassage, error) {
	return scanMode4ReviewPassage(r.pool.QueryRow(ctx, mode4PassageSelect+`
		WHERE user_id = $1
		ORDER BY generation_number DESC
		LIMIT 1
	`, userID))
}

func (r *Mode4ReviewRepository) CreatePassage(ctx context.Context, passage domain.Mode4ReviewPassage) (domain.Mode4ReviewPassage, error) {
	return scanMode4ReviewPassage(r.pool.QueryRow(ctx, `
		INSERT INTO mode4_review_passages (
			id, user_id, generation_number, word_ids_json, source_words_json, plain_passage_text,
			marked_passage_markdown, passage_spans_json, status, skip_count, last_skipped_at, completed_at, llm_run_id
		) VALUES (
			$1, $2, $3, $4::jsonb, $5::jsonb, $6,
			$7, $8::jsonb, $9, $10, $11, $12, $13
		)
		ON CONFLICT (user_id, generation_number) DO UPDATE SET
			word_ids_json = EXCLUDED.word_ids_json,
			source_words_json = EXCLUDED.source_words_json,
			plain_passage_text = EXCLUDED.plain_passage_text,
			marked_passage_markdown = EXCLUDED.marked_passage_markdown,
			passage_spans_json = EXCLUDED.passage_spans_json,
			status = EXCLUDED.status,
			skip_count = EXCLUDED.skip_count,
			last_skipped_at = EXCLUDED.last_skipped_at,
			completed_at = EXCLUDED.completed_at,
			llm_run_id = EXCLUDED.llm_run_id
		RETURNING id, user_id, generation_number, word_ids_json, source_words_json, plain_passage_text, marked_passage_markdown,
		          passage_spans_json, status, skip_count, last_skipped_at, completed_at, llm_run_id, created_at, updated_at
	`,
		passage.ID,
		passage.UserID,
		passage.GenerationNumber,
		marshalJSONValue(passage.WordIDs, "[]"),
		marshalJSONValue(passage.SourceWords, "[]"),
		passage.PlainPassageText,
		passage.MarkedPassageMarkdown,
		marshalJSONValue(passage.PassageSpans, "[]"),
		passage.Status,
		passage.SkipCount,
		passage.LastSkippedAt,
		passage.CompletedAt,
		passage.LLMRunID,
	))
}

func (r *Mode4ReviewRepository) UpdatePassageSkip(ctx context.Context, userID uuid.UUID, passageID uuid.UUID, skipCount int, skippedAt *time.Time) (domain.Mode4ReviewPassage, error) {
	return scanMode4ReviewPassage(r.pool.QueryRow(ctx, `
		UPDATE mode4_review_passages
		SET skip_count = $3,
		    last_skipped_at = $4,
		    status = 'active'
		WHERE user_id = $1
		  AND id = $2
		RETURNING id, user_id, generation_number, word_ids_json, source_words_json, plain_passage_text, marked_passage_markdown,
		          passage_spans_json, status, skip_count, last_skipped_at, completed_at, llm_run_id, created_at, updated_at
	`, userID, passageID, skipCount, skippedAt))
}

func (r *Mode4ReviewRepository) UpdatePassageStatus(ctx context.Context, userID uuid.UUID, passageID uuid.UUID, status domain.Mode4ReviewPassageStatus, completedAt *time.Time) (domain.Mode4ReviewPassage, error) {
	return scanMode4ReviewPassage(r.pool.QueryRow(ctx, `
		UPDATE mode4_review_passages
		SET status = $3,
		    completed_at = $4
		WHERE user_id = $1
		  AND id = $2
		RETURNING id, user_id, generation_number, word_ids_json, source_words_json, plain_passage_text, marked_passage_markdown,
		          passage_spans_json, status, skip_count, last_skipped_at, completed_at, llm_run_id, created_at, updated_at
	`, userID, passageID, status, completedAt))
}
