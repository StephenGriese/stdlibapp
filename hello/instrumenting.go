package hello

import (
	"context"
	kittymetrics "github.com/comcast-pulse/kitty/metrics"
	"time"
)

type instrumentingClient struct {
	kittymetrics.ServiceStatistics
	client Client
}

func WithInstrumentingClient(metrics kittymetrics.Factory, client Client) Client {
	return instrumentingClient{metrics.NewServiceStatistics(clientComponentName), client}
}

func (ic instrumentingClient) Hello(ctx context.Context, name string) (str string, err error) {
	defer func(begin time.Time) { ic.Update("Hello", begin, err) }(time.Now())

	return ic.client.Hello(ctx, name)
}

type instrumentingService struct {
	kittymetrics.ServiceStatistics
	service Service
}

func WithInstrumentingService(metrics kittymetrics.Factory, service Service) Service {
	return instrumentingService{metrics.NewServiceStatistics(serviceComponentName), service}
}

func (is instrumentingService) Hello(ctx context.Context, name string) (_ string, err error) {
	defer func(begin time.Time) { is.Update("Hello", begin, err) }(time.Now())

	return is.service.Hello(ctx, name)
}
