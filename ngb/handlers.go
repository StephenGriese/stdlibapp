package ngb

import (
	"context"
	"github.com/comcast-pulse/kitty/auth/jwt"
	kittyhttp "github.com/comcast-pulse/kitty/http"
	kittyserver "github.com/comcast-pulse/kitty/http/server"
	plog "github.com/comcast-pulse/log"
	"net/http"
)

const (
	pathLookup = "/lookup"
)

func NewRequestHandlers(logger plog.Logger, componentName, handlerName string, service Service) []kittyserver.RequestHandler {
	e := NewEndpoints(logger, componentName, service)

	return []kittyserver.RequestHandler{
		newLookupWordHandler(handlerName, e),
	}
}

func newLookupWordHandler(handlerName string, e Endpoints) kittyserver.RequestHandler {
	return kittyserver.NewRequestHandler(
		handlerName,
		e.NewLookupWordEndpoint(),
		decodeJSON[Request],
		kittyhttp.EncodeJSONResponse,
		pathLookup,
		[]string{http.MethodGet},
		[]jwt.Scope{AdminScope},
	)
}

func decodeJSON[T any](_ context.Context, r *http.Request) (any, error) {
	req := new(T)
	err := kittyhttp.DecodeJSONRequestBody(r, req)
	return *req, err
}
