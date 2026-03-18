package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"wordbit-advanced-app/backend/internal/domain"
)

type UserRepository struct {
	pool *pgxpool.Pool
}

func (r *UserRepository) GetOrCreateByExternalSubject(ctx context.Context, subject string, email string) (domain.User, error) {
	query := `
		INSERT INTO users (external_subject, email)
		VALUES ($1, $2)
		ON CONFLICT (external_subject) DO UPDATE
		SET email = CASE WHEN EXCLUDED.email <> '' THEN EXCLUDED.email ELSE users.email END,
		    last_active_at = NOW()
		RETURNING id, external_subject, email, created_at, updated_at, last_active_at
	`
	user, err := scanUser(r.pool.QueryRow(ctx, query, subject, email))
	if err != nil {
		return domain.User{}, err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO user_settings (user_id)
		VALUES ($1)
		ON CONFLICT (user_id) DO NOTHING
	`, user.ID)
	if err != nil {
		return domain.User{}, mapError(err)
	}
	return user, nil
}

func (r *UserRepository) TouchLastActive(ctx context.Context, userID uuid.UUID, at time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET last_active_at = $2 WHERE id = $1`, userID, at)
	return mapError(err)
}

func (r *UserRepository) ListActiveUsers(ctx context.Context, since time.Time) ([]domain.User, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, external_subject, email, created_at, updated_at, last_active_at
		FROM users
		WHERE last_active_at >= $1
		ORDER BY last_active_at DESC
	`, since)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var users []domain.User
	for rows.Next() {
		user, scanErr := scanUser(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		users = append(users, user)
	}
	return users, rows.Err()
}
