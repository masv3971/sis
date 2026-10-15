package memory

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/SUNET/sis/internal/core/domain"
)

// AccountRepository is an in-memory port.AccountRepository.
type AccountRepository struct {
	mu         sync.RWMutex
	byUsername map[string]domain.Account
}

// NewAccountRepository returns an empty in-memory account repository.
func NewAccountRepository() *AccountRepository {
	return &AccountRepository{byUsername: make(map[string]domain.Account)}
}

// GetByUsername returns an account by username.
func (r *AccountRepository) GetByUsername(_ context.Context, username string) (domain.Account, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.byUsername[username]
	if !ok {
		return domain.Account{}, domain.ErrNotFound
	}
	return a, nil
}

// GetByOIDC returns the account bound to an OIDC issuer+subject pair.
func (r *AccountRepository) GetByOIDC(_ context.Context, issuer, subject string) (domain.Account, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if subject == "" {
		return domain.Account{}, domain.ErrNotFound
	}
	for _, a := range r.byUsername {
		if a.OIDCIssuer == issuer && a.OIDCSubject == subject {
			return a, nil
		}
	}
	return domain.Account{}, domain.ErrNotFound
}

// Upsert inserts or updates by username, preserving id, created_at and the
// OIDC binding on update.
func (r *AccountRepository) Upsert(_ context.Context, a domain.Account) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.byUsername[a.Username]; ok {
		a.ID = existing.ID
		a.CreatedAt = existing.CreatedAt
		a.OIDCIssuer = existing.OIDCIssuer
		a.OIDCSubject = existing.OIDCSubject
	} else if a.ID == "" {
		a.ID = uuid.NewString()
	}
	r.byUsername[a.Username] = a
	return nil
}

// ClientRepository is an in-memory port.ClientRepository.
type ClientRepository struct {
	mu         sync.RWMutex
	byClientID map[string]domain.Client
}

// NewClientRepository returns an empty in-memory client repository.
func NewClientRepository() *ClientRepository {
	return &ClientRepository{byClientID: make(map[string]domain.Client)}
}

// GetByClientID returns a client by client_id.
func (r *ClientRepository) GetByClientID(_ context.Context, clientID string) (domain.Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.byClientID[clientID]
	if !ok {
		return domain.Client{}, domain.ErrNotFound
	}
	return c, nil
}

// Upsert inserts or updates by client_id, preserving created_at on update.
func (r *ClientRepository) Upsert(_ context.Context, c domain.Client) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.byClientID[c.ClientID]; ok {
		c.CreatedAt = existing.CreatedAt
	}
	r.byClientID[c.ClientID] = c
	return nil
}
