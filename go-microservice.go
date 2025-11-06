package go_microservice

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/sleepnfire/go-microservice/gin"
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
			Addr:    router.Port,
			Handler: router.Engine,
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
	fmt.Printf("%s's server is starting on port %s\n", as.name, as.port)
	if err := as.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Printf("Fail to start %s's server : %s\n", as.name, err)
	}
}

func (ms *MicroService) Start() {
	go ms.Public.startApiService()
	go ms.Internal.startApiService()
	go ms.Technical.startApiService()
}

func (as *ApiService) stopApiService() {
	fmt.Printf("%s's server shuting down \n", as.name)
	if err := as.server.Shutdown(context.Background()); err != nil {
		fmt.Printf("%s's server doesn't stop correctly : %s\n", as.name, err)
	}
}

func (ms *MicroService) Stop() error {
	ms.Public.stopApiService()
	ms.Internal.stopApiService()
	ms.Technical.stopApiService()
	return nil
}
