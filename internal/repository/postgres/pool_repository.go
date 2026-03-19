package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wordbit-advanced-app/backend/internal/domain"
)

type PoolRepository struct {
	pool *pgxpool.Pool
}

const poolItemSelect = `
	SELECT i.id, i.pool_id, i.user_id, i.word_id, i.ordinal, i.item_type, i.review_mode, i.due_at, i.status, i.is_review,
	       i.first_exposure_required, i.revealed_meaning, i.revealed_example, i.metadata, i.completed_at, i.created_at, i.updated_at,
	       w.id, w.word, w.normalized_form, w.canonical_form, w.lemma, w.word_family, w.confusable_group_key, w.part_of_speech, w.level, w.topic,
	       w.ipa, w.pronunciation_hint, w.vietnamese_meaning, w.english_meaning, w.example_sentence_1, w.example_sentence_2, w.source_provider, w.source_metadata,
	       w.created_at, w.updated_at
	FROM daily_learning_pool_items i
	JOIN words w ON w.id = i.word_id
`

func (r *PoolRepository) GetByLocalDate(ctx context.Context, userID uuid.UUID, localDate string) (domain.DailyLearningPool, []domain.DailyLearningPoolItem, error) {
	pool, err := scanPool(r.pool.QueryRow(ctx, `
		SELECT id, user_id, local_date, timezone, topic, due_review_count, short_term_count, weak_count, new_count, generated_at, created_at, updated_at
		FROM daily_learning_pools
		WHERE user_id = $1 AND local_date = $2::date
	`, userID, localDate))
	if err != nil {
		return domain.DailyLearningPool{}, nil, err
	}

	rows, err := r.pool.Query(ctx, poolItemSelect+`
		WHERE i.pool_id = $1
		ORDER BY i.ordinal ASC, i.created_at ASC
	`, pool.ID)
	if err != nil {
		return domain.DailyLearningPool{}, nil, mapError(err)
	}
	defer rows.Close()

	var items []domain.DailyLearningPoolItem
	for rows.Next() {
		item, scanErr := scanPoolItem(rows)
		if scanErr != nil {
			return domain.DailyLearningPool{}, nil, scanErr
		}
		items = append(items, item)
	}
	return pool, items, rows.Err()
}

func (r *PoolRepository) AcquireDailyPoolLock(ctx context.Context, userID uuid.UUID, localDate string) error {
	_, err := r.pool.Exec(ctx, `SELECT 1`)
	return mapError(err)
}

func (r *PoolRepository) CreatePoolWithItems(ctx context.Context, pool domain.DailyLearningPool, items []domain.DailyLearningPoolItem) (domain.DailyLearningPool, []domain.DailyLearningPoolItem, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.DailyLearningPool{}, nil, mapError(err)
	}
	defer tx.Rollback(ctx)

	lockKey := fmt.Sprintf("%s:%s", pool.UserID.String(), pool.LocalDate)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, lockKey); err != nil {
		return domain.DailyLearningPool{}, nil, mapError(err)
	}

	_, _, err = r.getByLocalDateTx(ctx, tx, pool.UserID, pool.LocalDate)
	if err == nil {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return domain.DailyLearningPool{}, nil, mapError(commitErr)
		}
		return r.GetByLocalDate(ctx, pool.UserID, pool.LocalDate)
	}
	if err != nil && err != domain.ErrNotFound {
		return domain.DailyLearningPool{}, nil, err
	}

	insertPoolQuery := `
		INSERT INTO daily_learning_pools (user_id, local_date, timezone, topic, due_review_count, short_term_count, weak_count, new_count, generated_at)
		VALUES ($1, $2::date, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, user_id, local_date, timezone, topic, due_review_count, short_term_count, weak_count, new_count, generated_at, created_at, updated_at
	`
	createdPool, err := scanPool(tx.QueryRow(ctx, insertPoolQuery,
		pool.UserID,
		pool.LocalDate,
		pool.Timezone,
		pool.Topic,
		pool.DueReviewCount,
		pool.ShortTermCount,
		pool.WeakCount,
		pool.NewCount,
		pool.GeneratedAt,
	))
	if err != nil {
		return domain.DailyLearningPool{}, nil, err
	}

	for i := range items {
		items[i].PoolID = createdPool.ID
		query := `
			INSERT INTO daily_learning_pool_items (
				pool_id, user_id, word_id, ordinal, item_type, review_mode, due_at, status,
				is_review, first_exposure_required, revealed_meaning, revealed_example, metadata, completed_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8,
				$9, $10, $11, $12, $13::jsonb, $14
			)
			RETURNING id, pool_id, user_id, word_id, ordinal, item_type, review_mode, due_at, status, is_review, first_exposure_required,
			          revealed_meaning, revealed_example, metadata, completed_at, created_at, updated_at
		`
		var metadata []byte
		err := tx.QueryRow(ctx, query,
			createdPool.ID,
			items[i].UserID,
			items[i].WordID,
			items[i].Ordinal,
			items[i].ItemType,
			items[i].ReviewMode,
			items[i].DueAt,
			items[i].Status,
			items[i].IsReview,
			items[i].FirstExposureRequired,
			items[i].RevealedMeaning,
			items[i].RevealedExample,
			fromJSONMap(items[i].Metadata),
			items[i].CompletedAt,
		).Scan(
			&items[i].ID,
			&items[i].PoolID,
			&items[i].UserID,
			&items[i].WordID,
			&items[i].Ordinal,
			&items[i].ItemType,
			&items[i].ReviewMode,
			&items[i].DueAt,
			&items[i].Status,
			&items[i].IsReview,
			&items[i].FirstExposureRequired,
			&items[i].RevealedMeaning,
			&items[i].RevealedExample,
			&metadata,
			&items[i].CompletedAt,
			&items[i].CreatedAt,
			&items[i].UpdatedAt,
		)
		if err != nil {
			return domain.DailyLearningPool{}, nil, mapError(err)
		}
		items[i].Metadata = toJSONMap(metadata)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.DailyLearningPool{}, nil, mapError(err)
	}
	return r.GetByLocalDate(ctx, pool.UserID, pool.LocalDate)
}

func (r *PoolRepository) GetNextActionableCard(ctx context.Context, userID uuid.UUID, localDate string, now time.Time) (*domain.DailyLearningPoolItem, error) {
	query := poolItemSelect + `
		JOIN daily_learning_pools p ON p.id = i.pool_id
		WHERE i.user_id = $1
		  AND p.local_date = $2::date
		  AND i.status = 'pending'
		  AND (i.due_at IS NULL OR i.due_at <= $3)
		ORDER BY i.ordinal ASC
		LIMIT 1
	`
	item, err := scanPoolItem(r.pool.QueryRow(ctx, query, userID, localDate, now))
	if err != nil {
		if err == domain.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (r *PoolRepository) GetPoolItem(ctx context.Context, userID uuid.UUID, itemID uuid.UUID) (domain.DailyLearningPoolItem, error) {
	return scanPoolItem(r.pool.QueryRow(ctx, poolItemSelect+`
		WHERE i.user_id = $1 AND i.id = $2
	`, userID, itemID))
}

func (r *PoolRepository) MarkPoolItemCompleted(ctx context.Context, itemID uuid.UUID, completedAt time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE daily_learning_pool_items
		SET status = 'completed', completed_at = $2
		WHERE id = $1
	`, itemID, completedAt)
	return mapError(err)
}

func (r *PoolRepository) UpdatePoolItemReveal(ctx context.Context, itemID uuid.UUID, kind domain.RevealKind) error {
	column := "revealed_meaning"
	if kind == domain.RevealKindExample {
		column = "revealed_example"
	}
	if kind == domain.RevealKindHint {
		return nil
	}
	_, err := r.pool.Exec(ctx, fmt.Sprintf(`
		UPDATE daily_learning_pool_items
		SET %s = TRUE
		WHERE id = $1
	`, column), itemID)
	return mapError(err)
}

func (r *PoolRepository) AppendPoolItem(ctx context.Context, item domain.DailyLearningPoolItem) (domain.DailyLearningPoolItem, error) {
	query := `
		INSERT INTO daily_learning_pool_items (
			pool_id, user_id, word_id, ordinal, item_type, review_mode, due_at, status,
			is_review, first_exposure_required, revealed_meaning, revealed_example, metadata, completed_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13::jsonb, $14
		)
		RETURNING id
	`
	if err := r.pool.QueryRow(ctx, query,
		item.PoolID,
		item.UserID,
		item.WordID,
		item.Ordinal,
		item.ItemType,
		item.ReviewMode,
		item.DueAt,
		item.Status,
		item.IsReview,
		item.FirstExposureRequired,
		item.RevealedMeaning,
		item.RevealedExample,
		fromJSONMap(item.Metadata),
		item.CompletedAt,
	).Scan(&item.ID); err != nil {
		return domain.DailyLearningPoolItem{}, mapError(err)
	}
	return r.GetPoolItem(ctx, item.UserID, item.ID)
}

func (r *PoolRepository) GetLastOrdinal(ctx context.Context, poolID uuid.UUID) (int, error) {
	var ordinal int
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(ordinal), 0)
		FROM daily_learning_pool_items
		WHERE pool_id = $1
	`, poolID).Scan(&ordinal)
	return ordinal, mapError(err)
}

func (r *PoolRepository) IncrementNewCount(ctx context.Context, poolID uuid.UUID, delta int) error {
	if delta == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE daily_learning_pools
		SET new_count = new_count + $2
		WHERE id = $1
	`, poolID, delta)
	return mapError(err)
}

func (r *PoolRepository) IncrementWeakCount(ctx context.Context, poolID uuid.UUID, delta int) error {
	if delta == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE daily_learning_pools
		SET weak_count = weak_count + $2
		WHERE id = $1
	`, poolID, delta)
	return mapError(err)
}

func (r *PoolRepository) DeleteItemsForUserWord(ctx context.Context, userID uuid.UUID, wordID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		WITH deleted AS (
			DELETE FROM daily_learning_pool_items
			WHERE user_id = $1 AND word_id = $2
			RETURNING pool_id, item_type
		),
		aggregated AS (
			SELECT pool_id,
			       COUNT(*) FILTER (WHERE item_type = 'review') AS review_count,
			       COUNT(*) FILTER (WHERE item_type = 'short_term') AS short_term_count,
			       COUNT(*) FILTER (WHERE item_type = 'weak') AS weak_count,
			       COUNT(*) FILTER (WHERE item_type = 'new') AS new_count
			FROM deleted
			GROUP BY pool_id
		)
		UPDATE daily_learning_pools p
		SET due_review_count = GREATEST(0, p.due_review_count - aggregated.review_count),
		    short_term_count = GREATEST(0, p.short_term_count - aggregated.short_term_count),
		    weak_count = GREATEST(0, p.weak_count - aggregated.weak_count),
		    new_count = GREATEST(0, p.new_count - aggregated.new_count)
		FROM aggregated
		WHERE p.id = aggregated.pool_id
	`, userID, wordID)
	return mapError(err)
}

func (r *PoolRepository) ForceDeleteByLocalDate(ctx context.Context, userID uuid.UUID, localDate string) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM daily_learning_pools
		WHERE user_id = $1 AND local_date = $2::date
	`, userID, localDate)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PoolRepository) getByLocalDateTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, localDate string) (domain.DailyLearningPool, []domain.DailyLearningPoolItem, error) {
	pool, err := scanPool(tx.QueryRow(ctx, `
		SELECT id, user_id, local_date, timezone, topic, due_review_count, short_term_count, weak_count, new_count, generated_at, created_at, updated_at
		FROM daily_learning_pools
		WHERE user_id = $1 AND local_date = $2::date
	`, userID, localDate))
	if err != nil {
		return domain.DailyLearningPool{}, nil, err
	}
	rows, err := tx.Query(ctx, poolItemSelect+` WHERE i.pool_id = $1 ORDER BY i.ordinal ASC`, pool.ID)
	if err != nil {
		return domain.DailyLearningPool{}, nil, mapError(err)
	}
	defer rows.Close()
	var items []domain.DailyLearningPoolItem
	for rows.Next() {
		item, scanErr := scanPoolItem(rows)
		if scanErr != nil {
			return domain.DailyLearningPool{}, nil, scanErr
		}
		items = append(items, item)
	}
	return pool, items, rows.Err()
}
