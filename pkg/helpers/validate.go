// Package helpers holds cross-cutting utilities. validate.go builds the shared
// struct validator: field names come from yaml/json tags so messages match the
// config file, and the custom validators below are available to any struct tag.
// Structure mirrors SUNET/vc's pkg/helpers/validate.go, reimplemented for sis.
package helpers

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
)

// NewValidator creates a validator whose error fields are named by the struct's
// yaml tag (falling back to json), with custom validators registered.
func NewValidator() (*validator.Validate, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())

	validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
		// Prefer yaml tag (used by config structs), fall back to json tag.
		name, _, _ := strings.Cut(fld.Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			name = strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		}
		if name == "" || name == "-" {
			return ""
		}
		return name
	})

	// httpurl - a URL with an http or https scheme and a host.
	if err := validate.RegisterValidation("httpurl", func(fl validator.FieldLevel) bool {
		u, ok := parseURL(fl.Field().String())
		if !ok {
			return false
		}
		scheme := strings.ToLower(u.Scheme)
		return (scheme == "http" || scheme == "https") && u.Host != ""
	}); err != nil {
		return nil, err
	}

	// https_endpoint - a server-side endpoint, configured by the operator, that
	// must be reached over TLS. No DNS resolution: these URLs come from the
	// deployment's own config, where an in-cluster host on a private address is
	// normal, and resolving at startup would make validation depend on DNS.
	if err := validate.RegisterValidation("https_endpoint", func(fl validator.FieldLevel) bool {
		u, ok := parseURL(fl.Field().String())
		if !ok {
			return false
		}
		// Hostname(), not Host: Host keeps the port, so "https://:443/x" has a
		// non-empty Host and no host at all.
		return strings.EqualFold(u.Scheme, "https") && u.Hostname() != ""
	}); err != nil {
		return nil, err
	}

	// redirect_uri - an OAuth 2.0 redirect URI. Per RFC 6749 it must have a
	// scheme and no fragment; per RFC 8252 custom (native app) schemes are
	// allowed. Never fetched server-side, so no DNS/SSRF check.
	if err := validate.RegisterValidation("redirect_uri", func(fl validator.FieldLevel) bool {
		u, ok := parseURL(fl.Field().String())
		if !ok || u.Scheme == "" || u.Fragment != "" {
			return false
		}
		if u.Scheme == "http" || u.Scheme == "https" {
			return u.Hostname() != ""
		}
		// Custom scheme: require content beyond the scheme.
		return u.Host != "" || u.Path != "" || u.Opaque != ""
	}); err != nil {
		return nil, err
	}

	// post_login_redirect_url - a parseable URL, absolute or relative, with no
	// fragment. The OIDC callback appends "#access_token=...", so a
	// destination that already has a fragment (or a bare trailing "#") would
	// bury the token where the web UI never looks for it.
	if err := validate.RegisterValidation("post_login_redirect_url", func(fl validator.FieldLevel) bool {
		raw := fl.Field().String()
		if strings.Contains(raw, "#") {
			return false
		}
		_, ok := parseURL(raw)
		return ok
	}); err != nil {
		return nil, err
	}

	// jwt_secret_production_min=N - the minimum JWT signing-secret length in
	// bytes when the root struct's Common.Production is true; a no-op
	// otherwise. Built-in conditional rules (required_if etc.) only see
	// sibling fields, so they cannot reach common.production from sis.auth;
	// Top() reads it from the root at any depth. A root without
	// Common.Production counts as non-production.
	if err := validate.RegisterValidation("jwt_secret_production_min", func(fl validator.FieldLevel) bool {
		prod, kind, _, ok := fl.GetStructFieldOKAdvanced2(fl.Top(), "Common.Production")
		if !ok || kind != reflect.Bool || !prod.Bool() {
			return true
		}
		n, err := strconv.Atoi(fl.Param())
		if err != nil {
			// A bad tag is a programming error, as for the built-in rules.
			panic(fmt.Sprintf("jwt_secret_production_min: bad param %q on %s", fl.Param(), fl.FieldName()))
		}
		return len(fl.Field().String()) >= n
	}); err != nil {
		return nil, err
	}

	// safe_uri - a URI fetched server-side, with SSRF prevention: blocks
	// localhost, loopback, link-local and private IPs. No fragment allowed.
	if err := validate.RegisterValidation("safe_uri", func(fl validator.FieldLevel) bool {
		u, ok := parseURL(fl.Field().String())
		if !ok || u.Scheme == "" || u.Fragment != "" {
			return false
		}
		host := u.Hostname()
		if host == "" || strings.EqualFold(host, "localhost") {
			return false
		}
		ips, err := net.LookupIP(host)
		if err != nil {
			return false
		}
		for _, ip := range ips {
			if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
				return false
			}
		}
		return true
	}); err != nil {
		return nil, err
	}

	return validate, nil
}

// Check validates s and, on failure, returns an error naming each offending
// field by its yaml/json tag.
func Check(s any) error {
	validate, err := NewValidator()
	if err != nil {
		return err
	}
	if err := validate.Struct(s); err != nil {
		var verrs validator.ValidationErrors
		if errors.As(err, &verrs) {
			return fmt.Errorf("validation failed: %s", formatValidationErrors(verrs))
		}
		return err
	}
	return nil
}

func parseURL(raw string) (*url.URL, bool) {
	if raw == "" {
		return nil, false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, false
	}
	return u, true
}

func formatValidationErrors(verrs validator.ValidationErrors) string {
	parts := make([]string, 0, len(verrs))
	for _, e := range verrs {
		if e.Param() != "" {
			parts = append(parts, fmt.Sprintf("%s failed %q (%s)", e.Namespace(), e.Tag(), e.Param()))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s failed %q", e.Namespace(), e.Tag()))
	}
	return strings.Join(parts, "; ")
}
