package hello

import (
	"context"
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
	hello  endpoint.Endpoint
}

func NewHTTPClient(logger plog.Logger, baseURL string, tokenService jwt.TokenService, opts ...kittyclient.Option) Client {
	logger = logger.WithComponent(clientComponentName)
	opts = append(opts, kittyclient.WithClientBefore(jwt.WithHTTPAuthHeader(tokenService)))

	// TODO use kittyClient.WithRetries like in vbom nona http_client.go

	hello := kittyclient.NewClient(
		naming.NewOperation(systemName, pathHello),
		http.MethodGet,
		url.MustAppend(url.MustParse(baseURL), pathHello),
		kittyhttp.EncodeJSONBody,
		kittyclient.NewJSONDecoder(
			logger,
			kittyclient.WithSystemName(systemName),
			kittyclient.WithOperationDescription("hello"),
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
		hello:  hello,
	}
}

func (client httpClient) Hello(ctx context.Context, name string) (string, error) {
	resp, err := client.hello(ctx, name)
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
