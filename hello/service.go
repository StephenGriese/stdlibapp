package hello

import (
	"context"
	plog "github.com/comcast-pulse/log"
)

const (
	serviceComponentName = "hello_service"
)

type Service interface {
	Hello(ctx context.Context, name string) (string, error)
}

type service struct {
	logger      plog.Logger
	id          string
	helloClient Client
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

func WithID(id string) ServiceOption {
	return func(s service) service {
		s.id = id
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

// WithHelloClient returns a ServiceOption that sets the helloClient on the service.
func WithHelloClient(helloClient Client) ServiceOption {
	return func(s service) service {
		s.helloClient = helloClient
		return s
	}
}

func (s service) Hello(ctx context.Context, name string) (string, error) {
	if s.helloClient == nil {
		return "Hello, World!", nil
	} else {
		return s.helloClient.Hello(ctx, name)
	}
}
