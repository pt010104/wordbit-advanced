package service

import (
	"context"

	"wordbit-advanced-app/backend/internal/domain"
)

type IdentityService struct {
	users UserRepository
	clock Clock
}

func NewIdentityService(users UserRepository, clock Clock) *IdentityService {
	return &IdentityService{users: users, clock: clock}
}

func (s *IdentityService) ResolveUser(ctx context.Context, subject AuthSubject) (domain.User, error) {
	user, err := s.users.GetOrCreateByExternalSubject(ctx, subject.Subject, subject.Email)
	if err != nil {
		return domain.User{}, err
	}
	if err := s.users.TouchLastActive(ctx, user.ID, s.clock.Now()); err != nil {
		return domain.User{}, err
	}
	user.LastActiveAt = s.clock.Now()
	return user, nil
}
