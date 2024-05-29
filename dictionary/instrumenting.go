package dictionary

import (
	"context"
	kittymetrics "github.com/comcast-pulse/kitty/metrics"
	"time"
)

type instrumentingClient struct {
	kittymetrics.ServiceStatistics
	client Client
}

func WithInstrumentingClient(metrics kittymetrics.Factory, clientComponentName string, client Client) Client {
	return instrumentingClient{metrics.NewServiceStatistics(clientComponentName), client}
}

func (ic instrumentingClient) LookupWord(ctx context.Context, name string) (str string, err error) {
	defer func(begin time.Time) { ic.Update("LookupWord", begin, err) }(time.Now())

	return ic.client.LookupWord(ctx, name)
}
