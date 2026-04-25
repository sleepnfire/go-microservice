# go-microservice

A Go library that provides a three-server HTTP microservice skeleton using [Gin](https://github.com/gin-gonic/gin).

It wires up three independent `gin.Engine` instances on fixed ports — **Public** (8080), **Internal** (8081), **Technical** (8082) — each with configurable logging middleware and panic recovery.

## Installation

```bash
go get github.com/sleepnfire/go-microservice
```

## Quick Start

```go
package main

import (
    "os"
    "os/signal"
    "syscall"

    goms "github.com/sleepnfire/go-microservice"
    mgin "github.com/sleepnfire/go-microservice/gin"
)

func main() {
    // Create three Gin engines with structured JSON logging
    gs := mgin.NewGinService(mgin.LogModeSlog)

    // Register routes on any of the three engines
    gs.Public.Engine.GET("/health", func(c *gin.Context) {
        c.JSON(200, gin.H{"status": "ok"})
    })
    gs.Internal.Engine.GET("/metrics", metricsHandler)
    gs.Technical.Engine.GET("/ready", readinessHandler)

    // Start all three servers concurrently
    ms := goms.NewMicroService(gs)
    ms.Start()

    // Block until SIGINT/SIGTERM, then gracefully shut down
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    ms.Stop()
}
```

## Server Ports

| Server    | Default Port | Typical Use                        |
|-----------|--------------|------------------------------------|
| Public    | `:8080`      | External-facing API                |
| Internal  | `:8081`      | Internal service-to-service routes |
| Technical | `:8082`      | Health checks, metrics, readiness  |

## Log Modes

Pass a `LogMode` constant to `NewGinService`:

| Constant       | Behaviour                                                                                      |
|----------------|-----------------------------------------------------------------------------------------------|
| `LogModeGin`   | Gin's built-in logger with a custom formatter that appends the server name and port to each line. |
| `LogModeSlog`  | Structured JSON logging via `log/slog`. Each request emits a JSON object with fields: `event`, `server`, `port`, `method`, `path`, `status`, `latency_ms`, `client_ip`. Panics are logged at `ERROR` level. |

## API Reference

### `gin` package (`github.com/sleepnfire/go-microservice/gin`)

```go
func NewGinService(mode LogMode) GinService
```

Returns a `GinService` with three pre-wired fields:

```go
type GinService struct {
    Public    ApiGin
    Internal  ApiGin
    Technical ApiGin
}

type ApiGin struct {
    Engine *gin.Engine
    Name   string
    Port   string
}
```

### Root package (`github.com/sleepnfire/go-microservice`)

```go
func NewMicroService(routers gin.GinService) *MicroService
func (ms *MicroService) Start()
func (ms *MicroService) Stop()
```

`Start()` launches the three servers in goroutines. `Stop()` calls `http.Server.Shutdown` on each sequentially.

## CI / Versioning

Each push to `main` runs tests and creates a SemVer tag automatically based on the first word of the commit message:

| First word (case-insensitive) | Version bump        |
|-------------------------------|---------------------|
| `fix`                         | Patch — `v1.2.3` → `v1.2.4` |
| `update` or `feature`         | Minor — `v1.2.3` → `v1.3.0` |
| `majeur`                      | Major — `v1.2.3` → `v2.0.0` |
| anything else                 | No tag created      |
