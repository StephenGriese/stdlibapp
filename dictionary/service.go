package dictionary

import (
	"context"
	"fmt"
	plog "github.com/comcast-pulse/log"
)

type Service interface {
	LookupWord(ctx context.Context, word string) (string, error)
}

type service struct {
	logger  plog.Logger
	appName string
	client  Client
}

var _ Service = &service{}

type ServiceOption func(service) service

func NewService(opts ...ServiceOption) Service {
	s := service{}
	for _, o := range opts {
		s = o(s)
	}
	return s
}

func WithAppName(appName string) ServiceOption {
	return func(s service) service {
		s.appName = appName
		return s
	}
}

// WithLogger returns a ServiceOption that sets the logger on the service.
func WithLogger(logger plog.Logger) ServiceOption {
	return func(s service) service {
		s.logger = logger
		return s
	}
}

// WithClient returns a ServiceOption that sets the client on the service.
func WithClient(client Client) ServiceOption {
	return func(s service) service {
		s.client = client
		return s
	}
}

func (s service) LookupWord(ctx context.Context, word string) (string, error) {
	s.logger.Info(ctx, "Looking up word", "word", word, "service_app_name", s.appName)
	if s.client == nil {
		s.logger.Info(ctx, "client is nil")
		return fmt.Sprintf("This is the definition of the word: %s", word), nil
	} else {
		return s.client.LookupWord(ctx, word)
	}
}
