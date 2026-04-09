// rrd.go
package rrd

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Resolutions
const (
	ResolutionDay   = 2 * time.Minute
	ResolutionWeek  = 15 * time.Minute
	ResolutionMonth = 1 * time.Hour

	SlotsDay   = 720 // 24h / 2min
	SlotsWeek  = 672 // 7d  / 15min
	SlotsMonth = 744 // 31d / 1h
)

// Ring is a circular buffer for timestamped measurements.
// Compact binary storage: 2×8 bytes per slot (int64 timestamp + int64 value).
type Ring struct {
	timestamps []*int64 // nil = empty slot
	values     []int64
	size       int
	head       int // next slot to write
}

func newRing(size int) *Ring {
	return &Ring{
		timestamps: make([]*int64, size),
		values:     make([]int64, size),
		size:       size,
	}
}

// push inserts a value. Overwrites the oldest slot if the ring is full.
func (r *Ring) push(ts time.Time, value int64) {
	t := ts.Unix()
	r.timestamps[r.head] = &t
	r.values[r.head] = value
	r.head = (r.head + 1) % r.size
}

// Sample is a measurement point returned to callers.
type Sample struct {
	At    time.Time
	Value int64
}

// ReadAll returns samples from most recent to oldest.
func (r *Ring) ReadAll() []Sample {
	out := make([]Sample, 0, r.size)
	// Traverse in reverse order to get most recent first
	for i := r.size - 1; i >= 0; i-- {
		idx := (r.head + i) % r.size
		if r.timestamps[idx] == nil {
			continue
		}
		out = append(out, Sample{
			At:    time.Unix(*r.timestamps[idx], 0),
			Value: r.values[idx],
		})
	}
	return out
}

// RRD manages the 3 rings and their automatic aggregation.
type RRD struct {
	mu   sync.RWMutex
	day  *Ring
	week *Ring
	mon  *Ring

	lastWeek  time.Time // timestamp of last 15min aggregate written
	lastMonth time.Time // timestamp of last 1h aggregate written

	path string // persistence file path (empty = no persistence)
}

func New(persistPath string) (*RRD, error) {
	rrd := &RRD{
		day:  newRing(SlotsDay),
		week: newRing(SlotsWeek),
		mon:  newRing(SlotsMonth),
		path: persistPath,
	}
	if persistPath != "" {
		if err := rrd.load(); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("rrd load: %w", err)
		}
	}
	return rrd, nil
}

// Record stores a new measurement at the current time.
// Call every 2 minutes.
func (rrd *RRD) Record(value int64) error {
	now := time.Now().Truncate(ResolutionDay)

	rrd.mu.Lock()
	defer rrd.mu.Unlock()

	// Day ring — always populated
	rrd.day.push(now, value)

	// Week ring — aggregation every 15 min (takes the last value)
	if now.Sub(rrd.lastWeek) >= ResolutionWeek {
		rrd.week.push(now, value)
		rrd.lastWeek = now
	}

	// Month ring — aggregation every hour
	if now.Sub(rrd.lastMonth) >= ResolutionMonth {
		rrd.mon.push(now, value)
		rrd.lastMonth = now
	}

	if rrd.path != "" {
		return rrd.save()
	}
	return nil
}

// QueryDay returns measurements for the last 24h (2 min resolution).
func (rrd *RRD) QueryDay() []Sample {
	rrd.mu.RLock()
	defer rrd.mu.RUnlock()
	return rrd.day.ReadAll()
}

// QueryWeek returns measurements for the last 7 days (15 min resolution).
func (rrd *RRD) QueryWeek() []Sample {
	rrd.mu.RLock()
	defer rrd.mu.RUnlock()
	return rrd.week.ReadAll()
}

// QueryMonth returns measurements for the last month (1h resolution).
func (rrd *RRD) QueryMonth() []Sample {
	rrd.mu.RLock()
	defer rrd.mu.RUnlock()
	return rrd.mon.ReadAll()
}
