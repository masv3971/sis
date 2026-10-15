// Package httpserver is the inbound HTTP transport: it builds the gin engine,
// declares the full route table, and runs it over an http.Server as a lifecycle
// service (New/Close). The handler logic lives in the internal/adapter/inbound/*
// packages — this package only wires routes to them.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/internal/adapter/inbound/authapi"
	"github.com/SUNET/sis/internal/adapter/inbound/eduapi"
	"github.com/SUNET/sis/internal/adapter/inbound/oeapi"
	"github.com/SUNET/sis/internal/adapter/inbound/restadmin"
	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"

	edudto "github.com/SUNET/sis/pkg/eduapi"
	oedto "github.com/SUNET/sis/pkg/oeapi"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// Guards are the per-operation authorization middlewares applied to API routes.
type Guards struct {
	Read  gin.HandlerFunc
	Write gin.HandlerFunc
}

// Service owns the gin engine and the HTTP server.
type Service struct {
	server *http.Server
	addr   net.Addr
	log    *logger.Log
}

// Addr returns the address the listener is bound to (useful with ":0").
func (s *Service) Addr() net.Addr { return s.addr }

// New builds the gin engine, declares every route, and starts serving in the
// background. It returns once the listener is accepting connections. Each
// inbound package exposes a *Client built here (passing cfg + log so handlers
// can log and read config); routes are wired to its methods.
func New(ctx context.Context, cfg *model.Cfg, authSvc *auth.Client, catalog *readmodel.Client, log *logger.Log) (*Service, error) {
	issuer := authSvc.Issuer()

	// Inbound handler clients. Each New returns (*Client, error) and the
	// handlers are methods on the returned Client, so routes read as
	// `engine.POST(path, authH.Login)` rather than passing deps per route.
	authH, err := authapi.New(ctx, cfg, authSvc, log)
	if err != nil {
		return nil, err
	}
	adminH, err := restadmin.New(ctx, cfg, catalog, log)
	if err != nil {
		return nil, err
	}
	oeH, err := oeapi.New(ctx, cfg, catalog, log)
	if err != nil {
		return nil, err
	}
	eduH, err := eduapi.New(ctx, cfg, catalog, log)
	if err != nil {
		return nil, err
	}

	gin.SetMode(gin.ReleaseMode)
	gin.EnableJsonDecoderDisallowUnknownFields() // reject unknown fields on writes

	engine := gin.New()
	engine.Use(requestLogger(log.New("request")), gin.Recovery(), bodyLimit(maxBodyBytes))

	engine.GET("/healthz", func(gc *gin.Context) {
		gc.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Unauthenticated auth endpoints: local login, client-credentials token,
	// and (when configured) OIDC RP login.
	engine.GET("/auth/config", authH.Config)
	engine.POST("/auth/login", authH.Login)
	engine.POST("/oauth/token", authH.Token)
	if authSvc.OIDCEnabled() {
		engine.GET("/auth/oidc/login", authH.OIDCLogin)
		engine.GET("/auth/oidc/callback", authH.OIDCCallback)
	}

	// Native /api/v1 requires a valid token; reads and writes gate on scope
	// (machine clients) or role (human logins).
	guards := Guards{
		Read:  auth.RequireScopeOrRole(domain.ScopeRead, string(domain.AuthRoleAdmin), string(domain.AuthRoleRegistrar), string(domain.AuthRoleReadOnly)),
		Write: auth.RequireScopeOrRole(domain.ScopeWrite, string(domain.AuthRoleAdmin), string(domain.AuthRoleRegistrar)),
	}
	api := engine.Group("/api/v1", auth.RequireAuth(issuer))
	api.GET("/persons", guards.Read, adminH.ListPersons)
	api.POST("/persons", guards.Write, adminH.CreatePerson)
	api.GET("/persons/:id", guards.Read, adminH.GetPerson)
	api.PUT("/persons/:id", guards.Write, adminH.UpdatePerson)
	api.DELETE("/persons/:id", guards.Write, adminH.DeletePerson)
	registerResource(api, guards, "organizations", adminH, catalog.Organizations)
	registerResource(api, guards, "academic-sessions", adminH, catalog.AcademicSessions)
	registerResource(api, guards, "courses", adminH, catalog.Courses)
	registerResource(api, guards, "offerings", adminH, catalog.Offerings)
	registerResource(api, guards, "enrollments", adminH, catalog.Enrollments)

	// Open Education API v6.0 read provider, behind the read scope.
	oe := engine.Group("/oeapi", auth.RequireAuth(issuer), guards.Read)
	oe.GET("/persons", oeH.ListPersons)
	oe.GET("/persons/:id", oeH.GetPerson)
	oe.GET("/organizations", oeapi.List(oeH, catalog.Organizations, oedto.FromOrganization))
	oe.GET("/organizations/:id", oeapi.Get(oeH, catalog.Organizations, oedto.FromOrganization))
	oe.GET("/academic-sessions", oeapi.List(oeH, catalog.AcademicSessions, oedto.FromAcademicSession))
	oe.GET("/academic-sessions/:id", oeapi.Get(oeH, catalog.AcademicSessions, oedto.FromAcademicSession))
	oe.GET("/courses", oeapi.List(oeH, catalog.Courses, oedto.FromCourse))
	oe.GET("/courses/:id", oeapi.Get(oeH, catalog.Courses, oedto.FromCourse))
	oe.GET("/offerings", oeapi.List(oeH, catalog.Offerings, oedto.FromOffering))
	oe.GET("/offerings/:id", oeapi.Get(oeH, catalog.Offerings, oedto.FromOffering))
	oe.GET("/associations", oeapi.List(oeH, catalog.Enrollments, oedto.FromAssociation))
	oe.GET("/associations/:id", oeapi.Get(oeH, catalog.Enrollments, oedto.FromAssociation))

	// Edu-API v1.0 read provider, behind the read scope.
	edu := engine.Group("/eduapi", auth.RequireAuth(issuer), guards.Read)
	edu.GET("/persons", eduH.ListPersons)
	edu.GET("/persons/:id", eduH.GetPerson)
	edu.GET("/orgs", eduapi.List(eduH, catalog.Organizations, "orgs", edudto.FromOrg))
	edu.GET("/orgs/:id", eduapi.Get(eduH, catalog.Organizations, "org", edudto.FromOrg))
	edu.GET("/academicSessions", eduapi.List(eduH, catalog.AcademicSessions, "academicSessions", edudto.FromAcademicSession))
	edu.GET("/academicSessions/:id", eduapi.Get(eduH, catalog.AcademicSessions, "academicSession", edudto.FromAcademicSession))
	edu.GET("/courses", eduapi.List(eduH, catalog.Courses, "courses", edudto.FromCourse))
	edu.GET("/courses/:id", eduapi.Get(eduH, catalog.Courses, "course", edudto.FromCourse))
	edu.GET("/classes", eduapi.List(eduH, catalog.Offerings, "classes", edudto.FromClass))
	edu.GET("/classes/:id", eduapi.Get(eduH, catalog.Offerings, "class", edudto.FromClass))
	edu.GET("/enrollments", eduapi.List(eduH, catalog.Enrollments, "enrollments", edudto.FromEnrollment))
	edu.GET("/enrollments/:id", eduapi.Get(eduH, catalog.Enrollments, "enrollment", edudto.FromEnrollment))

	// Built React SPA, when configured. Empty root means API-only (the dev
	// workflow runs `vite dev` on :5173 and proxies to this service).
	hc := cfg.SIS.HTTPServer
	if hc.Web.Root != "" {
		if err := mountSPA(engine, hc.Web.Root, log.New("spa")); err != nil {
			return nil, err
		}
	}

	s := &Service{
		log: log,
		server: &http.Server{
			Addr:         hc.Addr,
			Handler:      engine,
			ReadTimeout:  hc.ReadTimeout,
			WriteTimeout: hc.WriteTimeout,
		},
	}

	// Bind synchronously so an invalid or busy address fails New (and thus
	// startup) instead of only being logged from the serve goroutine.
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", hc.Addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", hc.Addr, err)
	}
	s.addr = ln.Addr()
	s.log.Info("sis listening", "addr", s.addr.String())

	go func() {
		if err := s.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("http server error", "err", err)
		}
	}()

	return s, nil
}

// registerResource declares the generic CRUD routes for entity type T at /path,
// guarding reads and writes with the supplied middleware. The generic resource
// handlers in restadmin cannot be methods on *Client (Go forbids type
// parameters on methods), so the Client is threaded through as the first arg.
func registerResource[T any, PT domain.EntityPtr[T]](rg *gin.RouterGroup, g Guards, path string, h *restadmin.Client, svc *service.Resource[T, PT]) {
	rg.GET("/"+path, g.Read, restadmin.ResourceList(h, svc))
	rg.POST("/"+path, g.Write, restadmin.ResourceCreate(h, svc))
	rg.GET("/"+path+"/:id", g.Read, restadmin.ResourceGet(h, svc))
	rg.PUT("/"+path+"/:id", g.Write, restadmin.ResourceUpdate(h, svc))
	rg.DELETE("/"+path+"/:id", g.Write, restadmin.ResourceDelete(h, svc))
}

// Close gracefully shuts down the HTTP server.
func (s *Service) Close(ctx context.Context) error {
	s.log.Info("stopping http server")
	return s.server.Shutdown(ctx)
}
