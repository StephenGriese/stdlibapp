package ngb

import (
	"context"
	"github.com/StephenGriese/stdlibapp/dictionary"
	plog "github.com/comcast-pulse/log"
	"time"
)

type loggingClient struct {
	logger plog.Logger
	client dictionary.Client
}

func WithLoggingClient(logger plog.Logger, clientComponentName string, client dictionary.Client) dictionary.Client {
	return loggingClient{logger.WithComponent(clientComponentName), client}
}

func (lc loggingClient) LookupWord(ctx context.Context, word string) (str string, err error) {
	defer func(begin time.Time) {
		lc.logger.LogCall(ctx, "LookupWord", begin, err,
			"word", word,
		)
	}(time.Now())

	return lc.client.LookupWord(ctx, word)
}

type loggingService struct {
	logger  plog.Logger
	service Service
}

func WithLoggingService(logger plog.Logger, serviceComponentName string, service Service) Service {
	return loggingService{logger.WithComponent(serviceComponentName), service}
}

func (ls loggingService) LookupWord(ctx context.Context, word string) (str string, err error) {
	defer func(begin time.Time) {
		ls.logger.LogCall(ctx, "LookupWord", begin, err,
			"word", word,
		)
	}(time.Now())

	return ls.service.LookupWord(ctx, word)
}
