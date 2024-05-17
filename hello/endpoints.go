package hello

import (
	"context"
	plog "github.com/comcast-pulse/log"
	"github.com/go-kit/kit/endpoint"
	"time"
)

const (
	componentName = "hello_endpoints"
)

type Endpoints interface {
	NewHelloEndpoint() endpoint.Endpoint
}

type endpoints struct {
	logger  plog.Logger
	service Service
}

func NewEndpoints(logger plog.Logger, service Service) Endpoints {
	return endpoints{
		logger:  logger.WithComponent(componentName),
		service: service,
	}
}

func (e endpoints) NewHelloEndpoint() endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (response interface{}, err error) {
		defer func(begin time.Time) {
			e.logger.LogCall(ctx, "Hello", begin, err, "request", request, "response", response)
		}(time.Now())

		req := request.(Request)

		// TODO Add validation?
		return e.service.Hello(ctx, req.Name)
	}
}
