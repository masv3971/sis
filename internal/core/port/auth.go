package port

import (
	"context"

	"github.com/SUNET/sis/internal/core/domain"
)

// AccountRepository is the persistence port for local login accounts.
type AccountRepository interface {
	GetByUsername(ctx context.Context, username string) (domain.Account, error)
	Upsert(ctx context.Context, a domain.Account) error
}

// ClientRepository is the persistence port for OAuth2 machine clients.
type ClientRepository interface {
	GetByClientID(ctx context.Context, clientID string) (domain.Client, error)
	Upsert(ctx context.Context, c domain.Client) error
}
