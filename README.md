# 🔄 redirector

redirector provides convenient http redirects.

## ⛳ global flags

### `-route <pattern> <destination> [optional flags]`

add a route. can be specified multiple times.

* `<pattern>` - must be {hostname}/{path} optionally containing a wildcard * character.
* `[path: bool; default=false]` - whether to forward the path from the original request.
* `[query: bool; default=false]` - whether to forward the query parameters from the original request.
* `[code: int; default=302]` - the http status code to set on redirects.

#### examples

- redirect all requests from www.example.com to example.com, preserving the original path and query parameters.
  
  `www.example.com/* example.com path query code=301`
- redirect blog from subdomain to subpath, appending the original path and discarding any query parameters.
  
  `blog.example.com/* example.com/blog path code=301`

## 💡 commands

### `(default)`

running redirector without a command starts an HTTP server at $PORT (default: 8080).

#### example

start an HTTP server that redirects any www.example.com requests to example.com. any other requests receive a 404 response.

```sh
redirector -route "www.example.com/* example.com path query code=301"
```

### 🌯 `wrap`

wrap a command and route any incoming HTTP requests that don't match any of the configured routes to it.

#### example

start an HTTP server that redirects any www.example.com requests to example.com. any other requests are forwarded to the "npm run serve" command. the command is run in the background once redirector boots up.

the wrapped command receives a $PORT env var that defaults to 8000. the command must start an http server on that port for redirector to forward requests to it. you may pass a -port flag to the wrap command to override.
  
```sh
redirector -route "www.example.com/* example.com path query code=301" wrap -- \
    npm run serve
```