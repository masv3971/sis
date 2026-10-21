// Package httpserver is the inbound HTTP transport: it builds the gin engine,
// declares the full route table, and runs it over an http.Server as a lifecycle
// service (New/Close). The handler logic lives in the internal/adapter/inbound/*
// packages — this package only wires routes to them.
package httpserver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	// Registers the generated /api/v1 OpenAPI document (make swagger).
	_ "github.com/SUNET/sis/docs/restadmin"

	"github.com/SUNET/sis/internal/adapter/inbound/authapi"
	"github.com/SUNET/sis/internal/adapter/inbound/convertapi"
	"github.com/SUNET/sis/internal/adapter/inbound/eduapi"
	"github.com/SUNET/sis/internal/adapter/inbound/oeapi"
	"github.com/SUNET/sis/internal/adapter/inbound/restadmin"
	"github.com/SUNET/sis/internal/auth"
	"github.com/SUNET/sis/internal/command"
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"

	edudto "github.com/SUNET/sis/pkg/eduapi"
	oedto "github.com/SUNET/sis/pkg/oeapi"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// Routes with their own body cap and write timeout
// (sis.adapter.inbound.convertapi.{max_body_bytes,write_timeout}).
const (
	parsePath    = "/api/v1/parse"
	populatePath = "/api/v1/populate"
)

// Guards are the per-operation authorization middlewares applied to API routes.
type Guards struct {
	Read  gin.HandlerFunc
	Write gin.HandlerFunc
}

// Service owns the gin engine and the HTTP server.
type Service struct {
	server *http.Server
	addr   net.Addr
	// cancelRequests cancels the base context every request context derives
	// from. Close calls it when the graceful drain deadline passes so handlers
	// blocked on downstream work (e.g. a postgres query) abort and release
	// their pooled connections before the outbound pool is closed.
	cancelRequests context.CancelFunc
	log            *logger.Log
}

// Addr returns the address the listener is bound to (useful with ":0").
func (s *Service) Addr() net.Addr { return s.addr }

// New builds the gin engine, declares every route, and starts serving in the
// background. It returns once the listener is accepting connections. Each
// inbound package exposes a *Client built here (passing cfg + log so handlers
// can log and read config); routes are wired to its methods.
func New(ctx context.Context, cfg *model.Cfg, verifier *auth.Verifier, catalog *readmodel.Client, commands *command.Client, log *logger.Log) (*Service, error) {
	// Inbound handler clients. Each New returns (*Client, error) and the
	// handlers are methods on the returned Client, so routes read as
	// `engine.GET(path, authAPI.Config)` rather than passing deps per route.
	authAPI, err := authapi.New(ctx, cfg)
	if err != nil {
		return nil, err
	}
	restAdmin, err := restadmin.New(ctx, cfg, catalog, commands, log)
	if err != nil {
		return nil, err
	}
	oeAPI, err := oeapi.New(ctx, cfg, catalog, log)
	if err != nil {
		return nil, err
	}
	eduAPI, err := eduapi.New(ctx, cfg, catalog, log)
	if err != nil {
		return nil, err
	}
	convertAPI, err := convertapi.New(ctx, cfg, commands, log)
	if err != nil {
		return nil, err
	}

	gin.SetMode(gin.ReleaseMode)
	gin.EnableJsonDecoderDisallowUnknownFields() // reject unknown fields on writes

	engine := gin.New()
	// Route on the escaped path and unescape parameters afterwards, so an id
	// or sourcedId containing "/" (sent as %2F) still matches a single :id
	// segment: /api/v1/persons/external%2F42 reaches GetPerson with
	// id="external/42" instead of 404ing on the decoded extra segment.
	engine.UseRawPath = true
	engine.UnescapePathValues = true
	// Document endpoints take whole documents, so they get their own cap.
	docLimit := cfg.SIS.Adapter.Inbound.ConvertAPI.MaxBodyBytes
	if docLimit <= 0 {
		docLimit = maxBodyBytes
	}
	docTimeout := cfg.SIS.Adapter.Inbound.ConvertAPI.WriteTimeout
	if docTimeout <= 0 {
		docTimeout = cfg.SIS.HTTPServer.WriteTimeout
	}
	reqLog := log.New("request")
	engine.Use(requestLogger(reqLog), gin.Recovery(), securityHeaders(cfg.SIS.HTTPServer.TLS.Enabled()),
		bodyLimit(maxBodyBytes, map[string]int64{
			parsePath:    docLimit,
			populatePath: docLimit,
		}),
		requestTimeout(cfg.SIS.HTTPServer.WriteTimeout, map[string]time.Duration{
			parsePath:    docTimeout,
			populatePath: docTimeout,
		}, reqLog),
	)

	engine.GET("/healthz", func(gc *gin.Context) {
		gc.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Public: where the web UI signs in. SIS issues no tokens.
	engine.GET("/auth/config", authAPI.Config)

	// Every API needs a token from a trusted issuer; reads need scope
	// sis.read and writes sis.write, whoever holds the token.
	guards := Guards{
		Read:  auth.RequireScope(auth.ScopeRead),
		Write: auth.RequireScope(auth.ScopeWrite),
	}
	requireAuth := verifier.RequireAuth()
	api := engine.Group("/api/v1", requireAuth)
	api.GET("/persons", guards.Read, restAdmin.ListPersons)
	api.POST("/persons", guards.Write, restAdmin.CreatePerson)
	api.GET("/persons/:id", guards.Read, restAdmin.GetPerson)
	api.PUT("/persons/:id", guards.Write, restAdmin.UpdatePerson)
	api.DELETE("/persons/:id", guards.Write, restAdmin.DeletePerson)
	registerResource(api, guards, "organizations", restAdmin, catalog.Organizations)
	registerResource(api, guards, "academic-sessions", restAdmin, catalog.AcademicSessions)
	registerResource(api, guards, "courses", restAdmin, catalog.Courses)
	registerResource(api, guards, "offerings", restAdmin, catalog.Offerings)
	registerResource(api, guards, "enrollments", restAdmin, catalog.Enrollments)
	registerResource(api, guards, "programs", restAdmin, catalog.Programs)
	registerResource(api, guards, "results", restAdmin, catalog.Results)

	// Document conversion: parse is pure (read guard), populate stores.
	api.POST("/parse", guards.Read, convertAPI.Parse)
	api.POST("/populate", guards.Write, convertAPI.Populate)

	// Open Education API v6.0 read provider, behind the read scope.
	oe := engine.Group("/oeapi", requireAuth, guards.Read)
	oe.GET("/persons", oeAPI.ListPersons)
	oe.GET("/persons/:id", oeAPI.GetPerson)
	oe.GET("/organizations", oeapi.List(oeAPI, catalog.Organizations, oedto.FromOrganization))
	oe.GET("/organizations/:id", oeapi.Get(oeAPI, catalog.Organizations, oedto.FromOrganization))
	oe.GET("/academic-sessions", oeapi.List(oeAPI, catalog.AcademicSessions, oedto.FromAcademicSession))
	oe.GET("/academic-sessions/:id", oeapi.Get(oeAPI, catalog.AcademicSessions, oedto.FromAcademicSession))
	oe.GET("/courses", oeapi.List(oeAPI, catalog.Courses, oedto.FromCourse))
	oe.GET("/courses/:id", oeapi.Get(oeAPI, catalog.Courses, oedto.FromCourse))
	oe.GET("/offerings", oeapi.List(oeAPI, catalog.Offerings, oedto.FromOffering))
	oe.GET("/offerings/:id", oeapi.Get(oeAPI, catalog.Offerings, oedto.FromOffering))
	oe.GET("/associations", oeapi.List(oeAPI, catalog.Enrollments, oedto.FromAssociation))
	oe.GET("/associations/:id", oeapi.Get(oeAPI, catalog.Enrollments, oedto.FromAssociation))
	oe.GET("/programs", oeapi.List(oeAPI, catalog.Programs, oedto.FromProgram))
	oe.GET("/programs/:id", oeapi.Get(oeAPI, catalog.Programs, oedto.FromProgram))
	oe.GET("/results", oeapi.List(oeAPI, catalog.Results, oedto.FromResult))
	oe.GET("/results/:id", oeapi.Get(oeAPI, catalog.Results, oedto.FromResult))

	// Edu-API v1.0 read provider, behind the read scope.
	edu := engine.Group("/eduapi", requireAuth, guards.Read)
	edu.GET("/persons", eduAPI.ListPersons)
	edu.GET("/persons/:id", eduAPI.GetPerson)
	edu.GET("/orgs", eduapi.List(eduAPI, catalog.Organizations, "orgs", edudto.FromOrg))
	edu.GET("/orgs/:id", eduapi.Get(eduAPI, catalog.Organizations, "org", edudto.FromOrg))
	edu.GET("/academicSessions", eduapi.List(eduAPI, catalog.AcademicSessions, "academicSessions", edudto.FromAcademicSession))
	edu.GET("/academicSessions/:id", eduapi.Get(eduAPI, catalog.AcademicSessions, "academicSession", edudto.FromAcademicSession))
	edu.GET("/courses", eduapi.List(eduAPI, catalog.Courses, "courses", edudto.FromCourse))
	edu.GET("/courses/:id", eduapi.Get(eduAPI, catalog.Courses, "course", edudto.FromCourse))
	edu.GET("/classes", eduapi.List(eduAPI, catalog.Offerings, "classes", edudto.FromClass))
	edu.GET("/classes/:id", eduapi.Get(eduAPI, catalog.Offerings, "class", edudto.FromClass))
	edu.GET("/enrollments", eduapi.List(eduAPI, catalog.Enrollments, "enrollments", edudto.FromEnrollment))
	edu.GET("/enrollments/:id", eduapi.Get(eduAPI, catalog.Enrollments, "enrollment", edudto.FromEnrollment))
	edu.GET("/programs", eduapi.List(eduAPI, catalog.Programs, "programs", edudto.FromProgram))
	edu.GET("/programs/:id", eduapi.Get(eduAPI, catalog.Programs, "program", edudto.FromProgram))
	edu.GET("/results", eduapi.List(eduAPI, catalog.Results, "results", edudto.FromResult))
	edu.GET("/results/:id", eduapi.Get(eduAPI, catalog.Results, "result", edudto.FromResult))

	httpCfg := cfg.SIS.HTTPServer

	// Swagger UI and the generated /api/v1 OpenAPI document (served at
	// /swagger/doc.json). Public: the spec reveals nothing a client could not
	// learn by probing, and every call made from the UI still needs a token.
	if httpCfg.Swagger.Enabled {
		engine.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.PersistAuthorization(true)))
	}

	// Built React SPA, when configured. Empty root means API-only (the dev
	// workflow runs `vite dev` on :5173 and proxies to this service).
	if httpCfg.Web.Root != "" {
		if err := mountSPA(engine, httpCfg.Web.Root, log.New("spa")); err != nil {
			return nil, err
		}
	}

	return start(ctx, httpCfg, engine, log)
}

// start binds the listener and serves handler in the background. Every
// request context derives from a base context owned by the Service, so Close
// can abort in-flight handlers once the graceful drain deadline passes.
func start(ctx context.Context, httpCfg model.HTTPServer, handler http.Handler, log *logger.Log) (*Service, error) {
	// Detach from ctx's cancellation: the server outlives New's ctx and is
	// stopped only by Close.
	baseCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s := &Service{
		log:            log,
		cancelRequests: cancel,
		server: &http.Server{
			Addr:         httpCfg.Addr,
			Handler:      handler,
			ReadTimeout:  httpCfg.ReadTimeout,
			WriteTimeout: httpCfg.WriteTimeout,
			BaseContext:  func(net.Listener) context.Context { return baseCtx },
		},
	}

	// Load the certificate before binding, so a bad file fails startup.
	if httpCfg.TLS.Enabled() {
		cert, err := tls.LoadX509KeyPair(httpCfg.TLS.CertFile, httpCfg.TLS.KeyFile)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("load tls certificate: %w", err)
		}
		s.server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	}

	// Bind synchronously so an invalid or busy address fails New (and thus
	// startup) instead of only being logged from the serve goroutine.
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", httpCfg.Addr)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("listen %s: %w", httpCfg.Addr, err)
	}
	s.addr = listener.Addr()
	if s.server.TLSConfig != nil {
		listener = tls.NewListener(listener, s.server.TLSConfig)
	}
	s.log.Info("sis listening", "addr", s.addr.String(), "tls", httpCfg.TLS.Enabled())

	go func() {
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error("http server error", "err", err)
		}
	}()

	return s, nil
}

// registerResource declares the generic CRUD routes for entity type T at /path,
// guarding reads and writes with the supplied middleware. The generic resource
// handlers in restadmin cannot be methods on *Client (Go forbids type
// parameters on methods), so the Client is threaded through as the first arg.
func registerResource[T any, PT domain.EntityPtr[T]](group *gin.RouterGroup, guards Guards, path string, admin *restadmin.Client, svc *service.Resource[T, PT]) {
	group.GET("/"+path, guards.Read, restadmin.ResourceList(admin, svc))
	group.POST("/"+path, guards.Write, restadmin.ResourceCreate(admin, svc))
	group.GET("/"+path+"/:id", guards.Read, restadmin.ResourceGet(admin, svc))
	group.PUT("/"+path+"/:id", guards.Write, restadmin.ResourceUpdate(admin, svc))
	group.DELETE("/"+path+"/:id", guards.Write, restadmin.ResourceDelete(admin, svc))
}

// Close gracefully shuts down the HTTP server, waiting for in-flight requests
// until ctx is done. If the deadline passes first, it cancels every request
// context (so handlers blocked on downstream I/O abort and release pooled
// resources) and force-closes the remaining connections, returning the
// deadline error. Close therefore never outlives ctx.
func (s *Service) Close(ctx context.Context) error {
	s.log.Info("stopping http server")
	defer s.cancelRequests()
	if err := s.server.Shutdown(ctx); err != nil {
		s.log.Warn("graceful shutdown deadline passed; aborting in-flight requests", "err", err)
		s.cancelRequests()
		return errors.Join(err, s.server.Close())
	}
	return nil
}
