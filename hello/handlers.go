package hello

import (
	"context"
	"github.com/comcast-pulse/kitty/auth/jwt"
	kittyhttp "github.com/comcast-pulse/kitty/http"
	kittyserver "github.com/comcast-pulse/kitty/http/server"
	plog "github.com/comcast-pulse/log"
	"net/http"
)

const (
	pathHello = "/hello"
)

func NewRequestHandlers(logger plog.Logger, service Service) []kittyserver.RequestHandler {
	e := NewEndpoints(logger, service)

	return []kittyserver.RequestHandler{
		newHelloHandler(e),
	}
}

func newHelloHandler(e Endpoints) kittyserver.RequestHandler {
	return kittyserver.NewRequestHandler(
		"hello",
		e.NewHelloEndpoint(),
		decodeJSON[Request],
		kittyhttp.EncodeJSONResponse,
		pathHello,
		[]string{http.MethodGet},
		[]jwt.Scope{AdminScope},
	)
}

func decodeJSON[T any](_ context.Context, r *http.Request) (any, error) {
	req := new(T)
	err := kittyhttp.DecodeJSONRequestBody(r, req)
	return *req, err
}
