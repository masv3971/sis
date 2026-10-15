package auth

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/SUNET/sis/internal/adapter/outbound/memory"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

func TestNewRejectsEphemeralSecretInProduction(t *testing.T) {
	cfg := &model.Cfg{Common: model.Common{Production: true}}
	_, err := New(context.Background(), cfg, memory.NewAccountRepository(), memory.NewClientRepository(),
		logger.NewWithWriter("test", false, "info", io.Discard))
	if !errors.Is(err, errEphemeralSecretInProduction) {
		t.Fatalf("expected errEphemeralSecretInProduction, got %v", err)
	}
}

func TestNewRejectsShortSecretInProduction(t *testing.T) {
	cfg := &model.Cfg{Common: model.Common{Production: true}}
	cfg.SIS.Auth.JWTSecret = strings.Repeat("a", model.MinProductionJWTSecretLen-1)
	_, err := New(context.Background(), cfg, memory.NewAccountRepository(), memory.NewClientRepository(),
		logger.NewWithWriter("test", false, "info", io.Discard))
	if !errors.Is(err, errShortSecretInProduction) {
		t.Fatalf("expected errShortSecretInProduction, got %v", err)
	}
}

func TestNewAcceptsMinLengthSecretInProduction(t *testing.T) {
	cfg := &model.Cfg{Common: model.Common{Production: true}}
	cfg.SIS.Auth.JWTSecret = strings.Repeat("a", model.MinProductionJWTSecretLen)
	if _, err := New(context.Background(), cfg, memory.NewAccountRepository(), memory.NewClientRepository(),
		logger.NewWithWriter("test", false, "info", io.Discard)); err != nil {
		t.Fatalf("32-byte secret should be accepted in production: %v", err)
	}
}

func TestNewAllowsEphemeralSecretInDev(t *testing.T) {
	cfg := &model.Cfg{}
	if _, err := New(context.Background(), cfg, memory.NewAccountRepository(), memory.NewClientRepository(),
		logger.NewWithWriter("test", false, "info", io.Discard)); err != nil {
		t.Fatalf("dev mode should fall back to an ephemeral secret: %v", err)
	}
}
