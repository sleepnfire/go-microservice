# go-microservice

A Go library that provides a three-server HTTP microservice skeleton using [Gin](https://github.com/gin-gonic/gin).

It wires up three independent `gin.Engine` instances — **Public** (8080), **Internal** (8081), **Technical** (8082) — each with request IDs, configurable logging and panic recovery, secure defaults and graceful shutdown.

## Installation

```bash
go get github.com/sleepnfire/go-microservice
```

Requires Go ≥ 1.27.3.

## Quick Start

```go
package main

import (
    "log"

    "github.com/gin-gonic/gin"
    goms "github.com/sleepnfire/go-microservice"
    mgin "github.com/sleepnfire/go-microservice/gin"
)

var version = "dev" // go build -ldflags "-X main.version=$(git describe --tags)"

func main() {
    gs := mgin.NewGinService(mgin.LogModeSlog,
        mgin.WithVersion(version),
        mgin.WithReadinessCheck(func() bool { return db.Ping() == nil }),
        mgin.WithMaxBodySize(1<<20),
        mgin.WithTrustedProxies("10.0.0.10"),          // your reverse proxy
        mgin.WithInternalPort("127.0.0.1:8081"),       // bind on loopback only
        mgin.WithTechnicalPort("127.0.0.1:8082"),
        mgin.WithPublicRoutes(func(r gin.IRouter) {
            r.GET("/hello", helloHandler)
        }),
    )

    ms := goms.NewMicroService(gs)
    ms.Start()

    // Blocks until SIGINT/SIGTERM or a server fails to start, then shuts down.
    if err := ms.WaitForShutdown(); err != nil {
        log.Fatal(err)
    }
}
```

Using your own lifecycle (uber/fx, errgroup…)? Call `ms.Start()` / `ms.Stop()` yourself and
select on `ms.Errors()` to detect a server that failed to bind.

## Servers

| Server    | Default address | Typical use                        | Built-in routes |
|-----------|-----------------|------------------------------------|-----------------|
| Public    | `:8080`         | External-facing API / web app      | — |
| Internal  | `:8081`         | Internal service-to-service routes | — |
| Technical | `:8082`         | Health checks, metrics, readiness  | `GET/HEAD /health`, `/ready`, `/version` |

The `With*Port` options take a full listen address: `":8081"` listens on all interfaces,
`"127.0.0.1:8081"` on loopback only (recommended for Internal/Technical when nothing remote needs them).

- `/health` — liveness, always `200 {"status":"ok"}`.
- `/ready` — `503` when the readiness check fails **or once `Stop()` has started**, so load balancers drain traffic first.
- `/version` — only when `WithVersion` is set.

All technical responses carry `Cache-Control: no-store`.

## Options

| Option | Effect |
|---|---|
| `WithPublicPort` / `WithInternalPort` / `WithTechnicalPort(addr)` | Listen address of each server |
| `WithReadinessCheck(func() bool)` | Backs `GET /ready` |
| `WithVersion(v)` | Exposes `GET /version` |
| `WithTrustedProxies(cidrs...)` | Proxies allowed to set `X-Forwarded-For` (all servers). **Default: none** |
| `WithSecureHeaders(bool)` | Security headers on Public (default `true`) |
| `WithHSTS(maxAge)` | Adds `Strict-Transport-Security` on Public (default off — HTTPS-only deployments) |
| `WithMaxBodySize(bytes)` | Body limit on Public, `413` when exceeded (default: none) |
| `WithPublicRoutes` / `WithInternalRoutes` / `WithTechnicalRoutes(fn)` | Register routes at construction |

## Log Modes

| Constant       | Behaviour |
|----------------|-----------|
| `LogModeGin`   | Gin's built-in logger with a custom formatter that appends the server name and port. |
| `LogModeSlog`  | JSON logging via `log/slog`: `event`, `request_id`, `server`, `port`, `method`, `path`, `query`, `status`, `latency_ms`, `bytes`, `client_ip`, `user_agent`. Sensitive query values (`code`, `state`, `token`, `access_token`, `password`, `api_key`…) are logged as `REDACTED`. Panics are logged at `ERROR` with the `request_id`. |
| `LogModeNoop`  | Recovery only, no request logging (tests). |

## Security & HTTP compliance defaults

- **Client IP**: no proxy is trusted by default, so `c.ClientIP()` returns the TCP peer and cannot be spoofed via `X-Forwarded-For`. Behind a reverse proxy, declare it with `WithTrustedProxies`.
- **Request ID**: `X-Request-ID` is propagated only if ≤ 128 chars of `[A-Za-z0-9._-]`; otherwise a UUID v4 (RFC 9562) is generated.
- **Public security headers**: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Content-Security-Policy: frame-ancestors 'none'`, `Referrer-Policy: strict-origin-when-cross-origin`, `Cross-Origin-Opener-Policy` / `Cross-Origin-Resource-Policy: same-origin`, `X-XSS-Protection: 0` (OWASP recommendation).
- **RFC 9110**: unsupported methods on a known path return `405` with an `Allow` header; technical routes answer `HEAD`; oversized bodies return `413`.
- **Server timeouts**: ReadHeader 5s, Read 10s, Write 30s, Idle 60s, shutdown 30s.

## Changelog

See [CHANGELOG.md](CHANGELOG.md). Releases that change behaviour list the required actions under **Action requise**.

## Migrating from v1.3 (to v1.4.1 — v1.4.0 is retracted)

- `ClientIP()` no longer trusts `X-Forwarded-For` by default → add `WithTrustedProxies(<proxy IPs>)` if you run behind HAProxy/nginx.
- Invalid incoming `X-Request-ID` values are replaced.
- Unknown methods on existing paths return `405` instead of `404`.
- `MaxBodySize` rejects requests whose `Content-Length` exceeds the limit with `413` before the handler runs.
- `/ready` returns `503` as soon as `Stop()` is called.

## CI / Versioning

Each push to `main` runs `go vet`, `go test -race` and `govulncheck`, then creates a SemVer tag based on the first word of the commit message (bare word, no colon: `feature …`, not `feature: …`).
Every tag gets a **GitHub Release** whose notes are the matching `CHANGELOG.md` section, so Renovate/Dependabot PRs in consuming projects show the **Action requise** items. For a tag pushed by hand, the release job runs on the tag push; for an existing tag, use *Actions → CI → Run workflow* with the tag name.

| First word (case-insensitive) | Version bump        |
|-------------------------------|---------------------|
| `fix`                         | Patch — `v1.2.3` → `v1.2.4` |
| `update` or `feature`         | Minor — `v1.2.3` → `v1.3.0` |
| `majeur`                      | Major — `v1.2.3` → `v2.0.0` |
| anything else                 | No tag created      |
