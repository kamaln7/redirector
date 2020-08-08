package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/fanyang01/radix"
	"github.com/kballard/go-shellquote"
)

var matcher = radix.NewPatternTrie()

type route struct {
	key   string
	dest  *url.URL
	code  int
	path  bool
	query bool
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	port = ":" + strings.TrimLeft(port, ":")
	mux := http.NewServeMux()
	mux.HandleFunc("/", handle)

	for _, env := range os.Environ() {
		pair := strings.SplitN(env, "=", 2)
		if len(pair) != 2 {
			continue
		}

		k, v := pair[0], pair[1]
		if strings.ToLower(strings.TrimRight(k, "0123456789")) != "route_" {
			continue
		}
		route, err := parseRoute(v)
		if err != nil {
			log.Printf("error parsing route %q = %q: %v\n", k, v, err)
			os.Exit(1)
		}
		_, _ = matcher.Add(route.key, route)
		log.Printf("configured route %q\n", route.key)
	}

	log.Println("listening on", port)
	log.Fatal(http.ListenAndServe(port, mux))
}

func parseRoute(config string) (*route, error) {
	parts, err := shellquote.Split(config)
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

	r := &route{key: parts[0], dest: u, code: 302}
	for _, part := range parts[2:] {
		if part == "path" {
			r.path = true
		} else if part == "query" {
			r.query = true
		} else if strings.HasPrefix(part, "code=") {
			code, err := strconv.Atoi(strings.TrimPrefix(part, "code="))
			if err != nil {
				return nil, fmt.Errorf("parsing code: %v", err)
			}
			r.code = code
		}
	}

	return r, nil
}

func handle(w http.ResponseWriter, r *http.Request) {
	key := r.Host + "/" + strings.Trim(r.URL.Path, "/")

	v, ok := matcher.Lookup(key)
	if !ok {
		log.Printf("request for key %q did not match any configured routes", key)
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	route, ok := v.(*route)
	if !ok {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	route.Execute(w, r)
}

func (r *route) Execute(w http.ResponseWriter, req *http.Request) {
	dest := *r.dest
	if r.path {
		dest.Path = path.Join(dest.Path, req.URL.Path)
	}
	if r.query {
		dest.RawQuery = req.URL.RawQuery
	}

	http.Redirect(w, req, dest.String(), r.code)
}

type stringSlice []string

var _ flag.Value = new(stringSlice)

func (ss *stringSlice) String() string {
	return strings.Join(*ss, ",")
}

func (ss *stringSlice) Set(value string) error {
	(*ss) = append(*ss, value)
	return nil
}
