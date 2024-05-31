package ngb

import (
	"context"
	"github.com/StephenGriese/stdlibapp/dictionary"
	kittymetrics "github.com/comcast-pulse/kitty/metrics"
	"time"
)

type instrumentingClient struct {
	kittymetrics.ServiceStatistics
	client dictionary.Client
}

func WithInstrumentingClient(metrics kittymetrics.Factory, clientComponentName string, client dictionary.Client) dictionary.Client {
	return instrumentingClient{metrics.NewServiceStatistics(clientComponentName), client}
}

func (ic instrumentingClient) LookupWord(ctx context.Context, name string) (str string, err error) {
	defer func(begin time.Time) { ic.Update("LookupWord", begin, err) }(time.Now())

	return ic.client.LookupWord(ctx, name)
}
