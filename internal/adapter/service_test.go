package adapter

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/SUNET/sis/internal/core/port"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"
)

// TestNewMemorySmoke wires the whole graph on the memory store: the file
// import lands in the catalog, the HTTP server answers, and Close stops it.
func TestNewMemorySmoke(t *testing.T) {
	ctx := t.Context()
	cfg := &model.Cfg{SIS: model.SIS{
		HTTPServer: model.HTTPServer{Addr: "127.0.0.1:0", ReadTimeout: time.Second, WriteTimeout: time.Second},
		Adapter:    model.Adapter{Sweeper: model.Sweeper{Interval: time.Minute}},
		FileImport: model.FileImport{Files: []string{"../../testdata/fake_students.json"}},
	}}
	log := logger.NewWithWriter("test", false, "error", io.Discard)

	s, err := New(ctx, cfg, log)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.outbound.pg != nil {
		t.Fatal("empty connection_string must select the memory store")
	}

	page, err := s.inbound.readmodel.Persons.ListPersons(ctx, port.ListParams{Limit: 1})
	if err != nil || page.Total == 0 {
		t.Fatalf("imported persons = %d, err %v; want the file import in the catalog", page.Total, err)
	}

	url := "http://" + s.inbound.http.Addr().String() + "/healthz"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz = %d", resp.StatusCode)
	}

	closeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.Close(closeCtx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if resp, err := http.Get(url); err == nil {
		resp.Body.Close()
		t.Fatal("server still answering after Close")
	}
}
