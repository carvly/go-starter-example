package domain

import (
	"log/slog"

	"app.local/internal/kit/uow"
)

type Service struct {
	users      Repository
	identities IdentityRepository
	openIDs    WeChatOpenIDRepository
	sessions   SessionRepository
	states     OAuthStateRepository
	publisher  Publisher
	issuer     AccessTokenIssuer
	uow        uow.UnitOfWork
	logger     *slog.Logger
	cfg        Config
}

func NewService(d Deps) *Service {
	return &Service{
		users:      d.Users,
		identities: d.Identities,
		openIDs:    d.OpenIDs,
		sessions:   d.Sessions,
		states:     d.States,
		publisher:  d.Publisher,
		issuer:     d.Issuer,
		uow:        d.UoW,
		logger:     d.Logger,
		cfg:        d.Config,
	}
}

func (s *Service) log() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}

const (
	DefaultListLimit = 20
	MaxListLimit     = 100
)

func normalizeLimit(limit int) int {
	if limit <= 0 || limit > MaxListLimit {
		return DefaultListLimit
	}
	return limit
}
