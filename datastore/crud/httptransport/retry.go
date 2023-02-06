package httptransport

import (
	"time"

	"github.com/cenkalti/backoff/v4"
)

var RetryPolicy = newPermanentRetryBackOff()

func newPermanentRetryBackOff() backoff.BackOff {
	exp := backoff.NewExponentialBackOff()
	exp.InitialInterval = 200 * time.Millisecond
	exp.RandomizationFactor = 0.2
	exp.MaxInterval = 60 * time.Second
	exp.MaxElapsedTime = 0
	return exp
}
