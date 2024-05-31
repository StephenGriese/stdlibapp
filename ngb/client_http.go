package ngb

import (
	"context"
	"github.com/StephenGriese/stdlibapp/dictionary"
	"github.com/comcast-pulse/kitty/auth/jwt"
	kittyhttp "github.com/comcast-pulse/kitty/http"
	kittyclient "github.com/comcast-pulse/kitty/http/client"

	"github.com/comcast-pulse/kitty/naming"
	"github.com/comcast-pulse/lang/url"
	plog "github.com/comcast-pulse/log"
	"github.com/go-kit/kit/endpoint"
	"net/http"
	"strings"
)

type httpClient struct {
	logger plog.Logger
	lookup endpoint.Endpoint
}

func NewHTTPClient(logger plog.Logger, systemName string, clientComponentName string, baseURL string, tokenService jwt.TokenService, opts ...kittyclient.Option) dictionary.Client {
	logger = logger.WithComponent(clientComponentName)
	opts = append(opts, kittyclient.WithClientBefore(jwt.WithHTTPAuthHeader(tokenService)))

	// TODO use kittyClient.WithRetries like in vbom nona http_client.go

	lookup := kittyclient.NewClient(
		naming.NewOperation(systemName, pathLookup),
		http.MethodGet,
		url.MustAppend(url.MustParse(baseURL), pathLookup),
		kittyhttp.EncodeJSONBody,
		kittyclient.NewJSONDecoder(
			logger,
			kittyclient.WithSystemName(systemName),
			kittyclient.WithOperationDescription("lookup word"),
			kittyclient.WithErrorResponseFactory(func() error {
				return new(errorResponse)
			}),
			kittyclient.WithSuccessFactory(func() interface{} {
				return new(string)
			}),
		).Decode,
		logger,
		opts...,
	).Endpoint()

	return httpClient{
		logger: logger,
		lookup: lookup,
	}
}

func (client httpClient) LookupWord(ctx context.Context, name string) (string, error) {
	req := Request{Word: name}
	resp, err := client.lookup(ctx, req)
	if err != nil {
		return "", err
	}
	return *resp.(*string), nil
}

type errorResponse struct {
	Errors []struct {
		Code, Title, Detail string
		Retryable           bool
	} `json:"errors"`
}

func (e errorResponse) Error() string {
	var sb strings.Builder
	for _, err := range e.Errors {
		if sb.Len() > 0 {
			sb.WriteString("; ")
		}
		sb.WriteString(err.Title)
		if err.Detail != "" {
			sb.WriteString(" - ")
			sb.WriteString(err.Detail)
		}
	}
	return sb.String()
}
