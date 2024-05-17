package hello

import (
	"context"
	plog "github.com/comcast-pulse/log"
	"time"
)

type loggingClient struct {
	logger plog.Logger
	client Client
}

func WithLoggingClient(logger plog.Logger, client Client) Client {
	return loggingClient{logger.WithComponent(clientComponentName), client}
}

func (lc loggingClient) Hello(ctx context.Context, name string) (str string, err error) {
	defer func(begin time.Time) {
		lc.logger.LogCall(ctx, "Hello", begin, err,
			"name", name,
		)
	}(time.Now())

	return lc.client.Hello(ctx, name)
}

type loggingService struct {
	logger  plog.Logger
	service Service
}

func WithLoggingService(logger plog.Logger, service Service) Service {
	return loggingService{logger.WithComponent(serviceComponentName), service}
}

func (ls loggingService) Hello(ctx context.Context, name string) (str string, err error) {
	defer func(begin time.Time) {
		ls.logger.LogCall(ctx, "Hello", begin, err,
			"name", name,
		)
	}(time.Now())

	return ls.service.Hello(ctx, name)
}
