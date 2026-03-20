// scheduler.go
package rrd

import (
	"context"
	"time"
)

// Collector est la fonction appelée pour obtenir la valeur courante (ex: espace disque libre).
type Collector func() (int64, error)

// StartScheduler lance la collecte périodique toutes les 2 minutes.
// S'arrête quand ctx est annulé.
func (rrd *RRD) StartScheduler(ctx context.Context, collect Collector) {
	ticker := alignedTicker(ResolutionDay) // démarre au prochain multiple de 2 min
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
				// relancer sur le prochain multiple de 2 min
				ticker.Reset(timeUntilNext(ResolutionDay))
			}
		}
	}()
}

// alignedTicker crée un ticker qui se déclenche au prochain multiple de d.
func alignedTicker(d time.Duration) *time.Ticker {
	return time.NewTicker(timeUntilNext(d))
}

func timeUntilNext(d time.Duration) time.Duration {
	now := time.Now()
	next := now.Truncate(d).Add(d)
	return next.Sub(now)
}
