package auth

import (
	"context"
	"errors"
	"io"
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

func TestNewAllowsEphemeralSecretInDev(t *testing.T) {
	cfg := &model.Cfg{}
	if _, err := New(context.Background(), cfg, memory.NewAccountRepository(), memory.NewClientRepository(),
		logger.NewWithWriter("test", false, "info", io.Discard)); err != nil {
		t.Fatalf("dev mode should fall back to an ephemeral secret: %v", err)
	}
}
