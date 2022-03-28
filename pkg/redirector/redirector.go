package redirector

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/fanyang01/radix"
	"github.com/kballard/go-shellquote"
)

// Route ...
type Route struct {
	Pattern     string
	Destination *url.URL
	Code        int
	CarryPath   bool
	CarryQuery  bool
}

// Redirector ...
type Redirector struct {
	matcher        *radix.PatternTrie
	defaultHandler http.Handler
}

// New creates a new Redirector
func New(routes []*Route, opts ...Option) *Redirector {
	r := &Redirector{
		matcher: radix.NewPatternTrie(),
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Option is a Redirector option
type Option func(*Redirector)

// WithDefaultHandler sets a default http handler for requests that don't match any of the configured routes.
// By default, Redirector will return a 404 error for such requests.
func WithDefaultHandler(h http.Handler) Option {
	return func(r *Redirector) {
		r.defaultHandler = h
	}
}

// NewRoute creates a new route from its string representation
// syntax: <pattern> <destination> [path: bool; default=false] [query: bool; default=false] [code: int; default=302]
func NewRoute(s string) (*Route, error) {
	parts, err := shellquote.Split(s)
	if err != nil {
		return nil, err
	}
	if len(parts) < 2 {
		return nil, errors.New("route must have at least a source and a destination")
	}
	dest := parts[1]
	u, err := url.Parse(dest)
	if err != nil {
		return nil, fmt.Errorf("parsing %q: %v", dest, err)
	}
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	if u.Host == "" {
		u.Host = u.Path
		u.Path = ""
	}

	r := &Route{Pattern: parts[0], Destination: u, Code: 302}
	for _, part := range parts[2:] {
		if part == "path" {
			r.CarryPath = true
		} else if part == "query" {
			r.CarryQuery = true
		} else if strings.HasPrefix(part, "code=") {
			code, err := strconv.Atoi(strings.TrimPrefix(part, "code="))
			if err != nil {
				return nil, fmt.Errorf("parsing code: %v", err)
			}
			r.Code = code
		}
	}

	return r, nil
}

// AddRoute configures a new route
func (r *Redirector) AddRoute(route *Route) error {
	_, has := r.matcher.Add(route.Pattern, route)
	if has {
		return fmt.Errorf("route already exists")
	}
	return nil
}

// Handler returns an http request handler
func (r *Redirector) Handler(w http.ResponseWriter, req *http.Request) {
	pattern := requestToRoutePattern(req)
	v, ok := r.matcher.Lookup(pattern)
	if !ok {
		// this request doesn't match any of the configured routes
		if r.defaultHandler != nil {
			r.defaultHandler.ServeHTTP(w, req)
		} else {
			log.Printf("request for %q did not match any configured routes", pattern)
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		}
		return
	}
	route, ok := v.(*Route)
	if !ok {
		// this should never happen
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	route.Execute(w, req)
}

// Execute executes a route according to its redirect rules
func (r *Route) Execute(w http.ResponseWriter, req *http.Request) {
	dest := r.Destination
	if r.CarryPath {
		dest.Path = path.Join(dest.Path, req.URL.Path)
	}
	if r.CarryQuery {
		dest.RawQuery = req.URL.RawQuery
	}

	http.Redirect(w, req, dest.String(), r.Code)
}

func requestToRoutePattern(r *http.Request) string {
	return r.Host + "/" + strings.Trim(r.URL.Path, "/")
}
