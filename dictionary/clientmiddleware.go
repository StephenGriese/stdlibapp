package dictionary

import (
	"fmt"
	"github.com/StephenGriese/stdlibapp/metrics"
	"net/http"
	"time"
)

type LoggingRoundTripper struct {
	Statistic metrics.ServiceStatistics
	Proxied   http.RoundTripper
}

func (lrt LoggingRoundTripper) RoundTrip(req *http.Request) (res *http.Response, e error) {
	// Do "before sending requests" actions here.
	defer func(begin time.Time) {
		lrt.Statistic.Update("LookupWord", begin, e)
	}(time.Now())

	// Send the request, get the response (or the error)
	res, e = lrt.Proxied.RoundTrip(req)

	// Handle the result.
	if e != nil {
		fmt.Printf("Error: %v", e)
	} else {
		fmt.Printf("Received %v response\n", res.Status)
	}

	return // TODO: fix the naked return
}
