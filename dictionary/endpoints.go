package dictionary

import (
	"context"
	plog "github.com/comcast-pulse/log"
	"github.com/go-kit/kit/endpoint"
	"time"
)

type Endpoints interface {
	NewLookupWordEndpoint() endpoint.Endpoint
}

type endpoints struct {
	logger        plog.Logger
	componentName string
	service       Service
}

func NewEndpoints(logger plog.Logger, componentName string, service Service) Endpoints {
	return endpoints{
		logger:        logger.WithComponent(componentName),
		service:       service,
		componentName: componentName,
	}
}

func (e endpoints) NewLookupWordEndpoint() endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (response interface{}, err error) {
		defer func(begin time.Time) {
			e.logger.LogCall(ctx, e.componentName, begin, err, "request", request, "response", response)
		}(time.Now())

		e.logger.Info(ctx, "QQQ NewLookupWordEndpoint called", "request", request)
		req := request.(Request)

		// FUTURE: Add validation?
		return e.service.LookupWord(ctx, req.Word)
	}
}
