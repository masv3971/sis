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
	"github.com/SUNET/sis/internal/core/domain"
	"github.com/SUNET/sis/internal/core/service"
	"github.com/SUNET/sis/internal/readmodel"
	"github.com/SUNET/sis/pkg/logger"
	"github.com/SUNET/sis/pkg/model"

	edudto "github.com/SUNET/sis/pkg/eduapi"
	oedto "github.com/SUNET/sis/pkg/oeapi"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// Routes with their own body cap (sis.adapter.inbound.convertapi.max_body_bytes).
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
func New(ctx context.Context, cfg *model.Cfg, verifier *auth.Verifier, catalog *readmodel.Client, log *logger.Log) (*Service, error) {
	// Inbound handler clients. Each New returns (*Client, error) and the
	// handlers are methods on the returned Client, so routes read as
	// `engine.GET(path, authH.Config)` rather than passing deps per route.
	authH, err := authapi.New(ctx, cfg)
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
	convH, err := convertapi.New(ctx, cfg, catalog, log)
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
	engine.Use(requestLogger(log.New("request")), gin.Recovery(), bodyLimit(maxBodyBytes, map[string]int64{
		parsePath:    docLimit,
		populatePath: docLimit,
	}))

	engine.GET("/healthz", func(gc *gin.Context) {
		gc.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Public: where the web UI signs in. SIS issues no tokens.
	engine.GET("/auth/config", authH.Config)

	// Every API needs a token from a trusted issuer; reads need scope
	// sis.read and writes sis.write, whoever holds the token.
	guards := Guards{
		Read:  auth.RequireScope(auth.ScopeRead),
		Write: auth.RequireScope(auth.ScopeWrite),
	}
	requireAuth := verifier.RequireAuth()
	api := engine.Group("/api/v1", requireAuth)
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
	registerResource(api, guards, "programs", adminH, catalog.Programs)
	registerResource(api, guards, "results", adminH, catalog.Results)

	// Document conversion: parse is pure (read guard), populate stores.
	api.POST("/parse", guards.Read, convH.Parse)
	api.POST("/populate", guards.Write, convH.Populate)

	// Open Education API v6.0 read provider, behind the read scope.
	oe := engine.Group("/oeapi", requireAuth, guards.Read)
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
	oe.GET("/programs", oeapi.List(oeH, catalog.Programs, oedto.FromProgram))
	oe.GET("/programs/:id", oeapi.Get(oeH, catalog.Programs, oedto.FromProgram))
	oe.GET("/results", oeapi.List(oeH, catalog.Results, oedto.FromResult))
	oe.GET("/results/:id", oeapi.Get(oeH, catalog.Results, oedto.FromResult))

	// Edu-API v1.0 read provider, behind the read scope.
	edu := engine.Group("/eduapi", requireAuth, guards.Read)
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
	edu.GET("/programs", eduapi.List(eduH, catalog.Programs, "programs", edudto.FromProgram))
	edu.GET("/programs/:id", eduapi.Get(eduH, catalog.Programs, "program", edudto.FromProgram))
	edu.GET("/results", eduapi.List(eduH, catalog.Results, "results", edudto.FromResult))
	edu.GET("/results/:id", eduapi.Get(eduH, catalog.Results, "result", edudto.FromResult))

	hc := cfg.SIS.HTTPServer

	// Swagger UI and the generated /api/v1 OpenAPI document (served at
	// /swagger/doc.json). Public: the spec reveals nothing a client could not
	// learn by probing, and every call made from the UI still needs a token.
	if hc.Swagger.Enabled {
		engine.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.PersistAuthorization(true)))
	}

	// Built React SPA, when configured. Empty root means API-only (the dev
	// workflow runs `vite dev` on :5173 and proxies to this service).
	if hc.Web.Root != "" {
		if err := mountSPA(engine, hc.Web.Root, log.New("spa")); err != nil {
			return nil, err
		}
	}

	return start(ctx, hc, engine, log)
}

// start binds the listener and serves handler in the background. Every
// request context derives from a base context owned by the Service, so Close
// can abort in-flight handlers once the graceful drain deadline passes.
func start(ctx context.Context, hc model.HTTPServer, handler http.Handler, log *logger.Log) (*Service, error) {
	// Detach from ctx's cancellation: the server outlives New's ctx and is
	// stopped only by Close.
	baseCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s := &Service{
		log:            log,
		cancelRequests: cancel,
		server: &http.Server{
			Addr:         hc.Addr,
			Handler:      handler,
			ReadTimeout:  hc.ReadTimeout,
			WriteTimeout: hc.WriteTimeout,
			BaseContext:  func(net.Listener) context.Context { return baseCtx },
		},
	}

	// Load the certificate before binding, so a bad file fails startup.
	if hc.TLS.Enabled() {
		cert, err := tls.LoadX509KeyPair(hc.TLS.CertFile, hc.TLS.KeyFile)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("load tls certificate: %w", err)
		}
		s.server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	}

	// Bind synchronously so an invalid or busy address fails New (and thus
	// startup) instead of only being logged from the serve goroutine.
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", hc.Addr)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("listen %s: %w", hc.Addr, err)
	}
	s.addr = ln.Addr()
	if s.server.TLSConfig != nil {
		ln = tls.NewListener(ln, s.server.TLSConfig)
	}
	s.log.Info("sis listening", "addr", s.addr.String(), "tls", hc.TLS.Enabled())

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
