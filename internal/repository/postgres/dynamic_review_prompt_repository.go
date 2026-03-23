package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"wordbit-advanced-app/backend/internal/domain"
)

type DynamicReviewPromptRepository struct {
	pool *pgxpool.Pool
}

func (r *DynamicReviewPromptRepository) ListByLocalDate(ctx context.Context, userID uuid.UUID, localDate string) ([]domain.DailyDynamicReviewPrompt, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, local_date, word_id, review_mode, payload_json, llm_run_id, created_at, updated_at
		FROM daily_dynamic_review_prompts
		WHERE user_id = $1
		  AND local_date = $2::date
		ORDER BY created_at ASC, word_id ASC
	`, userID, localDate)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var prompts []domain.DailyDynamicReviewPrompt
	for rows.Next() {
		prompt, scanErr := scanDynamicReviewPrompt(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		prompts = append(prompts, prompt)
	}
	return prompts, rows.Err()
}

func (r *DynamicReviewPromptRepository) UpsertBatch(ctx context.Context, prompts []domain.DailyDynamicReviewPrompt) ([]domain.DailyDynamicReviewPrompt, error) {
	if len(prompts) == 0 {
		return []domain.DailyDynamicReviewPrompt{}, nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	defer tx.Rollback(ctx)

	saved := make([]domain.DailyDynamicReviewPrompt, 0, len(prompts))
	for _, prompt := range prompts {
		row := tx.QueryRow(ctx, `
			INSERT INTO daily_dynamic_review_prompts (
				id, user_id, local_date, word_id, review_mode, payload_json, llm_run_id
			) VALUES (
				$1, $2, $3::date, $4, $5, $6::jsonb, $7
			)
			ON CONFLICT (user_id, local_date, word_id, review_mode) DO UPDATE SET
				payload_json = EXCLUDED.payload_json,
				llm_run_id = EXCLUDED.llm_run_id
			RETURNING id, user_id, local_date, word_id, review_mode, payload_json, llm_run_id, created_at, updated_at
		`,
			prompt.ID,
			prompt.UserID,
			prompt.LocalDate,
			prompt.WordID,
			prompt.ReviewMode,
			marshalJSONValue(prompt.Payload, "{}"),
			prompt.LLMRunID,
		)
		savedPrompt, scanErr := scanDynamicReviewPrompt(row)
		if scanErr != nil {
			return nil, scanErr
		}
		saved = append(saved, savedPrompt)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, mapError(err)
	}
	return saved, nil
}
