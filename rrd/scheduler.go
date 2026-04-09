// scheduler.go
package rrd

import (
	"context"
	"time"
)

// Collector is the function called to get the current value (e.g., free disk space).
type Collector func() (int64, error)

// StartScheduler starts periodic collection every 2 minutes.
// Stops when ctx is cancelled.
func (rrd *RRD) StartScheduler(ctx context.Context, collect Collector) {
	ticker := alignedTicker(ResolutionDay) // starts at next multiple of 2 min
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if v, err := collect(); err == nil {
					rrd.Record(v)
				}
				// restart at next multiple of 2 min
				ticker.Reset(timeUntilNext(ResolutionDay))
			}
		}
	}()
}

// alignedTicker creates a ticker that fires at the next multiple of d.
func alignedTicker(d time.Duration) *time.Ticker {
	return time.NewTicker(timeUntilNext(d))
}

func timeUntilNext(d time.Duration) time.Duration {
	now := time.Now()
	next := now.Truncate(d).Add(d)
	return next.Sub(now)
}
