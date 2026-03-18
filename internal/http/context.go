package apihttp

import (
	"context"

	"wordbit-advanced-app/backend/internal/domain"
)

type contextKey string

const userContextKey contextKey = "authenticated_user"

func withUser(ctx context.Context, user domain.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

func UserFromContext(ctx context.Context) (domain.User, bool) {
	user, ok := ctx.Value(userContextKey).(domain.User)
	return user, ok
}
