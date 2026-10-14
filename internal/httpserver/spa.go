package httpserver

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/SUNET/sis/pkg/logger"
)

// apiPrefixes lists path prefixes that MUST never be swallowed by the SPA
// catch-all. A request under any of these that reaches NoRoute is a genuine
// 404 (unknown API endpoint) and must stay a 404 JSON response — otherwise a
// typo in an API path would return the SPA, which browsers would render and
// which API clients would see as a 200 with HTML in the body.
var apiPrefixes = []string{
	"/api/",
	"/auth/",
	"/oauth/",
	"/oeapi/",
	"/eduapi/",
	"/healthz",
}

// mountSPA wires the built React bundle at root onto engine: /assets/* serves
// hashed bundle files directly, well-known root files (favicon, robots) are
// served when present, and any other GET for a non-API path returns
// index.html so client-side routing / deep links work. Returns an error only
// when root is set but unreadable; a missing index.html is treated as a
// configuration error since nothing useful would be served otherwise.
func mountSPA(engine *gin.Engine, root string, log *logger.Log) error {
	indexPath := filepath.Join(root, "index.html")
	info, err := os.Stat(indexPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return &os.PathError{Op: "stat", Path: indexPath, Err: os.ErrInvalid}
	}

	assetsDir := filepath.Join(root, "assets")
	engine.Static("/assets", assetsDir)

	// Vite emits a handful of root-level files (favicon, vite.svg, manifest).
	// Serve any that are present without failing if they are not.
	for _, name := range []string{"favicon.ico", "favicon.svg", "robots.txt", "manifest.webmanifest", "vite.svg"} {
		p := filepath.Join(root, name)
		if _, err := os.Stat(p); err == nil {
			engine.StaticFile("/"+name, p)
		}
	}

	engine.NoRoute(func(gc *gin.Context) {
		// Only GET/HEAD can serve the SPA; other methods on unknown paths
		// stay 404 so misdirected writes do not get a 200 HTML.
		if gc.Request.Method != http.MethodGet && gc.Request.Method != http.MethodHead {
			gc.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		// API paths that fall through to NoRoute are honest-to-goodness 404s;
		// returning HTML there would mask routing bugs.
		path := gc.Request.URL.Path
		for _, p := range apiPrefixes {
			if strings.HasPrefix(path, p) {
				gc.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
				return
			}
		}
		gc.File(indexPath)
	})

	log.Info("serving SPA", "root", root)
	return nil
}
