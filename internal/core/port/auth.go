package port

import (
	"context"

	"github.com/SUNET/sis/internal/core/domain"
)

// AccountRepository is the persistence port for login accounts.
type AccountRepository interface {
	GetByUsername(ctx context.Context, username string) (domain.Account, error)
	// GetByOIDC returns the account bound to an OIDC issuer+subject pair.
	GetByOIDC(ctx context.Context, issuer, subject string) (domain.Account, error)
	// Upsert inserts or updates by username. The OIDC binding is written on
	// insert only, so re-seeding an account never changes who can use it.
	Upsert(ctx context.Context, a domain.Account) error
	// Create inserts a new account and never modifies an existing one. It
	// returns domain.ErrConflict if the username, id or OIDC issuer+subject
	// is already taken.
	Create(ctx context.Context, a domain.Account) error
}

// ClientRepository is the persistence port for OAuth2 machine clients.
type ClientRepository interface {
	GetByClientID(ctx context.Context, clientID string) (domain.Client, error)
	Upsert(ctx context.Context, c domain.Client) error
}
