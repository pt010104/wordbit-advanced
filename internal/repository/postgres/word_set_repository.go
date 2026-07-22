package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wordbit-advanced-app/backend/internal/domain"
)

type WordSetRepository struct {
	pool *pgxpool.Pool
}

func (r *WordSetRepository) List(ctx context.Context, userID uuid.UUID) ([]domain.WordSet, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, name, icon, mode, is_default, auto_generate_new_words, enabled_review_modes, created_at, updated_at
		FROM word_sets
		WHERE user_id = $1
		ORDER BY is_default DESC, lower(name) ASC
	`, userID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var sets []domain.WordSet
	for rows.Next() {
		set, scanErr := scanWordSet(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		sets = append(sets, set)
	}
	return sets, rows.Err()
}

func (r *WordSetRepository) Get(ctx context.Context, userID uuid.UUID, setID uuid.UUID) (domain.WordSet, error) {
	return scanWordSet(r.pool.QueryRow(ctx, `
		SELECT id, user_id, name, icon, mode, is_default, auto_generate_new_words, enabled_review_modes, created_at, updated_at
		FROM word_sets
		WHERE user_id = $1 AND id = $2
	`, userID, setID))
}

func (r *WordSetRepository) GetDefault(ctx context.Context, userID uuid.UUID) (domain.WordSet, error) {
	return scanWordSet(r.pool.QueryRow(ctx, `
		SELECT id, user_id, name, icon, mode, is_default, auto_generate_new_words, enabled_review_modes, created_at, updated_at
		FROM word_sets
		WHERE user_id = $1 AND is_default = TRUE
	`, userID))
}

func (r *WordSetRepository) Create(ctx context.Context, set domain.WordSet) (domain.WordSet, error) {
	mode := set.Mode
	if mode == "" {
		mode = domain.WordSetModeCustom
	}
	name := strings.TrimSpace(set.Name)
	if name == "" {
		return domain.WordSet{}, errors.New("word set name is required")
	}
	return scanWordSet(r.pool.QueryRow(ctx, `
		INSERT INTO word_sets (user_id, name, icon, mode, is_default, auto_generate_new_words, enabled_review_modes)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, user_id, name, icon, mode, is_default, auto_generate_new_words, enabled_review_modes, created_at, updated_at
	`, set.UserID, name, set.Icon, string(mode), set.IsDefault, set.AutoGenerateNewWords, reviewModeValues(set.EnabledReviewModes)))
}

func (r *WordSetRepository) Update(ctx context.Context, set domain.WordSet) (domain.WordSet, error) {
	mode := set.Mode
	if mode == "" {
		mode = domain.WordSetModeCustom
	}
	name := strings.TrimSpace(set.Name)
	if name == "" {
		return domain.WordSet{}, errors.New("word set name is required")
	}
	return scanWordSet(r.pool.QueryRow(ctx, `
		UPDATE word_sets
		SET name = $3, icon = $4, mode = $5,
		    auto_generate_new_words = $6, enabled_review_modes = $7
		WHERE user_id = $1 AND id = $2
		RETURNING id, user_id, name, icon, mode, is_default, auto_generate_new_words, enabled_review_modes, created_at, updated_at
	`, set.UserID, set.ID, name, set.Icon, string(mode), set.AutoGenerateNewWords, reviewModeValues(set.EnabledReviewModes)))
}

func (r *WordSetRepository) Delete(ctx context.Context, userID uuid.UUID, setID uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mapError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// A word belongs to a user through user_word_states.  Do not leave those
	// states unowned when a custom set is removed: EnsureDefault deliberately
	// assigns legacy unowned states to Default, which would otherwise make the
	// deleted set's words appear in the Default dictionary and study pool.
	// Keep the global words rows; they may be shared by another user or set.
	_, err = tx.Exec(ctx, `
		WITH deleted AS (
			DELETE FROM daily_learning_pool_items item
			USING user_word_states state
			WHERE item.user_id = $1
			  AND state.user_id = $1
			  AND state.word_set_id = $2
			  AND item.word_id = state.word_id
			RETURNING item.pool_id, item.item_type
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
		UPDATE daily_learning_pools pool
		SET due_review_count = GREATEST(0, pool.due_review_count - aggregated.review_count),
		    short_term_count = GREATEST(0, pool.short_term_count - aggregated.short_term_count),
		    weak_count = GREATEST(0, pool.weak_count - aggregated.weak_count),
		    new_count = GREATEST(0, pool.new_count - aggregated.new_count)
		FROM aggregated
		WHERE pool.id = aggregated.pool_id
	`, userID, setID)
	if err != nil {
		return mapError(err)
	}

	_, err = tx.Exec(ctx, `
		DELETE FROM user_word_states
		WHERE user_id = $1 AND word_set_id = $2
	`, userID, setID)
	if err != nil {
		return mapError(err)
	}

	tag, err := tx.Exec(ctx, `
		DELETE FROM word_sets
		WHERE user_id = $1 AND id = $2 AND is_default = FALSE
	`, userID, setID)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return tx.Commit(ctx)
}

func (r *WordSetRepository) EnsureDefault(ctx context.Context, userID uuid.UUID) (domain.WordSet, error) {
	existing, err := r.GetDefault(ctx, userID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.WordSet{}, err
	}
	return scanWordSet(r.pool.QueryRow(ctx, `
		INSERT INTO word_sets (user_id, name, icon, mode, is_default, auto_generate_new_words)
		VALUES ($1, 'Default', 'default', 'new_words', TRUE, TRUE)
		ON CONFLICT DO NOTHING
		RETURNING id, user_id, name, icon, mode, is_default, auto_generate_new_words, enabled_review_modes, created_at, updated_at
	`, userID))
}

func scanWordSet(row pgx.Row) (domain.WordSet, error) {
	var set domain.WordSet
	var mode string
	var reviewModes []string
	err := row.Scan(
		&set.ID,
		&set.UserID,
		&set.Name,
		&set.Icon,
		&mode,
		&set.IsDefault,
		&set.AutoGenerateNewWords,
		&reviewModes,
		&set.CreatedAt,
		&set.UpdatedAt,
	)
	set.Mode = domain.WordSetMode(mode)
	set.EnabledReviewModes = make([]domain.ReviewMode, 0, len(reviewModes))
	for _, reviewMode := range reviewModes {
		set.EnabledReviewModes = append(set.EnabledReviewModes, domain.ReviewMode(reviewMode))
	}
	return set, mapError(err)
}

func reviewModeValues(modes []domain.ReviewMode) []string {
	values := make([]string, 0, len(modes))
	for _, mode := range modes {
		values = append(values, string(mode))
	}
	return values
}
