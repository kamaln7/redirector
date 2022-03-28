package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/kamaln7/redirector/pkg/redirector"
)

var cliUsage = func() {}

func main() {
	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		fmt.Printf("💡 using port %s from $PORT env var\n", p)
		port = p
	}
	var redirectorOpts []redirector.Option

	// cli handling
	var routes strslice
	fs := flag.NewFlagSet("", flag.ExitOnError)
	fs.Var(&routes, "route", `add a route. can be specified multiple times.

syntax: <pattern> <destination> [path: bool; default=false] [query: bool; default=false] [code: int; default=302]
	<pattern> - must be {hostname}/{path} optionally containing a wildcard * character.
	
example routes:
	- redirect all requests from www.example.com to example.com, preserving the original path and query parameters.
	  www.example.com/* example.com path query code=301
	- redirect blog from subdomain to subpath, appending the original path and preserving query parameters.
	  blog.example.com/* example.com/blog path query code=301`)
	cliUsage = func() {
		fmt.Printf(`🔄 redirector

redirector provides convenient http redirects.

💡 commands

  - (default) running redirector without a command starts an HTTP server at $PORT (default: 8080).

    example: start an HTTP server that redirects any www.example.com requests to example.com. any other requests receive
      a 404 response.

        redirector -route "www.example.com/* example.com path query code=301"

  - wrap: wrap a command and route any incoming HTTP requests that don't match any of the configured routes to it.

    example: start an HTTP server that redirects any www.example.com requests to example.com. any other requests are
      forwarded to the "npm run serve" command. the command is run in the background once redirector boots up.

      the wrapped command receives a $PORT env var that defaults to 8000. the command must start an http server on that
      port for redirector to forward requests to it. you may pass a -port flag to the wrap command to override.
  
          redirector -route "www.example.com/* example.com path query code=301" wrap -- \
            npm run serve

⛳ global flags

`)
		fs.PrintDefaults()
	}
	fs.Usage = cliUsage
	fs.Parse(os.Args[1:])
	var (
		args    = fs.Args()
		command string
	)
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}
	switch command {
	case "":
		// default behavior, redirect only
		go func() {
			chanSig := make(chan os.Signal, 1)
			signal.Notify(chanSig, os.Interrupt, syscall.SIGTERM)
			sig := <-chanSig
			fmt.Printf("❗ got %s, shutting down...\n", sig)
			os.Exit(0)
		}()
	case "wrap":
		// wrap another command that starts an http server and use it as the default handler
		wc, err := NewWrapCommand(args)
		if err != nil {
			fmt.Printf("🚨 creating wrapped command: %v\n", err)
			os.Exit(1)
		}
		if fmt.Sprint(wc.Port()) == port {
			fmt.Printf("🚨 the wrapped command's port cannot be the same as the redirector's.\n")
			os.Exit(1)
		}
		redirectorOpts = append(redirectorOpts, wc.RedirectorDefaultHandler())

		// start the command
		go func() {
			chanSig := make(chan os.Signal, 1)
			signal.Notify(chanSig)
			fmt.Printf("🤖 starting wrapped command\n\n")
			err := wc.Run(chanSig)
			fmt.Println("") // add a newline after the command's output
			if err == nil {
				fmt.Printf("✅ command exited cleanly. Shutting down...\n")
				os.Exit(0)
			}
			fmt.Printf("🚨 %v\n", err)
			exitCode := 1
			var exErr *exec.ExitError
			if errors.As(err, &exErr) {
				exitCode = exErr.ExitCode() // mirror the command's exit code
			}
			os.Exit(exitCode)
		}()
	default:
		fmt.Printf("🚨 unrecognized command %s\n", command)
		os.Exit(1)
	}

	// create redirector
	re := redirector.New(nil, redirectorOpts...)
	hasErr := false
	for _, route := range routes {
		r, err := redirector.NewRoute(route)
		if err != nil {
			fmt.Printf("❌ parsing route %q: %v\n", route, err)
			hasErr = true
		}
		err = re.AddRoute(r)
		if err != nil {
			fmt.Printf("❌ adding route %q: %v\n", route, err)
			hasErr = true
		}
	}
	if hasErr {
		os.Exit(1)
	}

	// start http
	mux := http.NewServeMux()
	mux.HandleFunc("/", re.Handler)
	port = ":" + port
	fmt.Printf("🚀 redirector running on %s\n", port)
	if err := http.ListenAndServe(port, mux); err != nil {
		fmt.Printf("🚨 %v\n", err)
		os.Exit(1)
	}
}

// WrapCommand is the `wrap` command
type WrapCommand struct {
	cmd  *exec.Cmd
	port uint
}

func NewWrapCommand(args []string) (*WrapCommand, error) {
	wc := &WrapCommand{}

	fs := flag.NewFlagSet("wrap", flag.ExitOnError)
	fs.UintVar(&wc.port, "port", 8000, "the port that the wrapped command will listen on")
	fs.Usage = func() {
		cliUsage()
		fmt.Printf(`
🌯⛳ wrap flags

`)
		fs.PrintDefaults()
	}
	fs.Parse(args)
	cmdLine := fs.Args()
	if len(cmdLine) > 0 && cmdLine[0] == "--" {
		cmdLine = cmdLine[1:]
	}
	if len(cmdLine) == 0 {
		return nil, fmt.Errorf("a command must be set")
	}

	wc.cmd = exec.Command(cmdLine[0], cmdLine[1:]...)
	wc.cmd.Stdin = os.Stdin
	wc.cmd.Stdout = os.Stdout
	wc.cmd.Stderr = os.Stderr
	wc.cmd.Env = make([]string, len(os.Environ())+1)
	copy(wc.cmd.Env, os.Environ())
	// set a PORT={wrapped command port} env
	wc.cmd.Env = append(wc.cmd.Env, fmt.Sprintf("PORT=%d", wc.port))

	return wc, nil
}

// Port returns the configured port
func (wc *WrapCommand) Port() uint {
	return wc.port
}

// RedirectorDefaultHandler returns a redirector.WithDefaultHandler option that forwards requests to the wrapped command.
func (wc *WrapCommand) RedirectorDefaultHandler() redirector.Option {
	u, _ := url.Parse(fmt.Sprintf("http://localhost:%d", wc.port))
	proxy := httputil.NewSingleHostReverseProxy(u)
	return redirector.WithDefaultHandler(proxy)
}

// Run runs the command
func (wc *WrapCommand) Run(chanSig chan os.Signal) error {
	if chanSig != nil {
		go func() {
			for sig := range chanSig {
				_ = wc.cmd.Process.Signal(sig)
			}
		}()
	}
	return wc.cmd.Run()
}

var _ flag.Value = new(strslice)

type strslice []string

func (i *strslice) String() string {
	return fmt.Sprint([]string(*i))
}

func (i *strslice) Set(value string) error {
	*i = append(*i, value)
	return nil
}
