package go_microservice

import (
	"context"
	"errors"
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
	}
}

func (as *ApiService) startApiService() {
	slog.Info("server starting", "server", as.name, "port", as.port)
	if err := as.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server failed to start", "server", as.name, "error", err)
	}
}

func (ms *MicroService) Start() {
	go ms.Public.startApiService()
	go ms.Internal.startApiService()
	go ms.Technical.startApiService()
}

func (as *ApiService) stopApiService(ctx context.Context) error {
	slog.Info("server shutting down", "server", as.name)
	if err := as.server.Shutdown(ctx); err != nil {
		slog.Error("server failed to stop", "server", as.name, "error", err)
		return err
	}
	return nil
}

func (ms *MicroService) Stop() error {
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

// WaitForShutdown blocks until SIGINT or SIGTERM is received, then stops all servers.
func (ms *MicroService) WaitForShutdown() error {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	return ms.Stop()
}
