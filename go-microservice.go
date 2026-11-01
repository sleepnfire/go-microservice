package go_microservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sleepnfire/go-microservice/gin"
)

const (
	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadTimeout       = 10 * time.Second
	defaultWriteTimeout      = 30 * time.Second
	defaultIdleTimeout       = 60 * time.Second
	defaultShutdownTimeout   = 30 * time.Second
)

type ApiService struct {
	server http.Server
	name   string
	port   string
}

type MicroService struct {
	Public    ApiService
	Internal  ApiService
	Technical ApiService

	routers gin.GinService
	errs    chan error
}

func NewApiService(router gin.ApiGin) ApiService {
	return ApiService{
		server: http.Server{
			Addr:              router.Port,
			Handler:           router.Engine,
			ReadHeaderTimeout: defaultReadHeaderTimeout,
			ReadTimeout:       defaultReadTimeout,
			WriteTimeout:      defaultWriteTimeout,
			IdleTimeout:       defaultIdleTimeout,
		},
		name: router.Name,
		port: router.Port,
	}
}

func NewMicroService(routers gin.GinService) *MicroService {
	return &MicroService{
		Public:    NewApiService(routers.Public),
		Internal:  NewApiService(routers.Internal),
		Technical: NewApiService(routers.Technical),
		routers:   routers,
		errs:      make(chan error, 3),
	}
}

func (as *ApiService) startApiService(errs chan<- error) {
	slog.Info("server starting", "server", as.name, "port", as.port)
	if err := as.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server failed to start", "server", as.name, "error", err)
		if errs != nil {
			errs <- fmt.Errorf("%s server (%s): %w", as.name, as.port, err)
		}
	}
}

// Start launches the three servers in background goroutines. Failures to
// listen (e.g. port already in use) are reported on Errors().
func (ms *MicroService) Start() {
	go ms.Public.startApiService(ms.errs)
	go ms.Internal.startApiService(ms.errs)
	go ms.Technical.startApiService(ms.errs)
}

// Errors returns a channel receiving fatal server errors (e.g. bind failure).
// Consumers using their own lifecycle (fx, errgroup…) should select on it.
func (ms *MicroService) Errors() <-chan error {
	return ms.errs
}

func (as *ApiService) stopApiService(ctx context.Context) error {
	slog.Info("server shutting down", "server", as.name)
	if err := as.server.Shutdown(ctx); err != nil {
		slog.Error("server failed to stop", "server", as.name, "error", err)
		return err
	}
	return nil
}

// Stop marks the service as not ready (GET /ready → 503) then gracefully
// shuts down the three servers concurrently.
func (ms *MicroService) Stop() error {
	ms.routers.SetShuttingDown()

	ctx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
	defer cancel()

	var wg sync.WaitGroup
	errs := make([]error, 3)
	services := [3]*ApiService{&ms.Public, &ms.Internal, &ms.Technical}

	for i := range services {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = services[idx].stopApiService(ctx)
		}(i)
	}
	wg.Wait()
	return errors.Join(errs...)
}

// WaitForShutdown blocks until SIGINT or SIGTERM is received, or until a
// server fails to start, then stops all servers. A startup failure is
// returned joined with any shutdown error.
func (ms *MicroService) WaitForShutdown() error {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	var startErr error
	select {
	case <-quit:
	case startErr = <-ms.errs:
	}
	return errors.Join(startErr, ms.Stop())
}
