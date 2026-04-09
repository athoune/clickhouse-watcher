package rrd

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestNewRRD(t *testing.T) {
	rrd, err := New("")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if rrd == nil {
		t.Fatal("New returned nil")
	}
}

func TestRingPushAndRead(t *testing.T) {
	ring := newRing(5)
	now := time.Now()

	ring.push(now, 100)
	ring.push(now.Add(time.Minute), 200)
	ring.push(now.Add(2*time.Minute), 300)

	samples := ring.ReadAll()
	if len(samples) != 3 {
		t.Errorf("Expected 3 samples, got %d", len(samples))
	}

	// Samples are now from most recent to oldest
	if samples[0].Value != 300 {
		t.Errorf("Expected first (most recent) value 300, got %d", samples[0].Value)
	}
	if samples[2].Value != 100 {
		t.Errorf("Expected last (oldest) value 100, got %d", samples[2].Value)
	}
}

func TestRingWrapAround(t *testing.T) {
	size := 3
	ring := newRing(size)
	now := time.Now()

	for i := 0; i < size*2; i++ {
		ring.push(now.Add(time.Duration(i)*time.Minute), int64(i))
	}

	samples := ring.ReadAll()
	if len(samples) != size {
		t.Errorf("Expected %d samples (ring full), got %d", size, len(samples))
	}

	// Samples are now from most recent to oldest
	first := samples[0]
	if first.Value != 5 {
		t.Errorf("Expected first (most recent) value 5, got %d", first.Value)
	}
}

func TestRecordBasic(t *testing.T) {
	rrd, _ := New("")

	rrd.Record(1000)
	rrd.Record(2000)
	rrd.Record(3000)

	day := rrd.QueryDay()
	if len(day) != 3 {
		t.Errorf("Expected 3 day samples, got %d", len(day))
	}

	// Samples are now from most recent to oldest
	if day[0].Value != 3000 {
		t.Errorf("Expected first (most recent) value 3000, got %d", day[0].Value)
	}
}

func TestRecordWeekAggregation(t *testing.T) {
	rrd, _ := New("")

	now := time.Now().Truncate(ResolutionWeek)

	for i := 0; i < 8; i++ {
		rrd.mu.Lock()
		rrd.day.push(now.Add(time.Duration(i)*ResolutionDay), int64(100*i))
		rrd.mu.Unlock()
	}

	rrd.mu.Lock()
	rrd.day.push(now, 5000)
	rrd.lastWeek = now.Add(-ResolutionWeek)
	rrd.mu.Unlock()

	rrd.Record(5000)

	week := rrd.QueryWeek()
	if len(week) != 1 {
		t.Errorf("Expected 1 week sample, got %d", len(week))
	}
}

func TestRecordMonthAggregation(t *testing.T) {
	rrd, _ := New("")

	now := time.Now().Truncate(ResolutionMonth)

	rrd.mu.Lock()
	rrd.lastMonth = now.Add(-ResolutionMonth)
	rrd.mu.Unlock()

	rrd.Record(10000)

	mon := rrd.QueryMonth()
	if len(mon) != 1 {
		t.Errorf("Expected 1 month sample, got %d", len(mon))
	}
}

func TestQueryDay(t *testing.T) {
	rrd, _ := New("")

	now := time.Now()
	for i := 0; i < 10; i++ {
		rrd.mu.Lock()
		rrd.day.push(now.Add(time.Duration(i)*time.Minute), int64(i*100))
		rrd.mu.Unlock()
	}

	day := rrd.QueryDay()
	if len(day) != 10 {
		t.Errorf("Expected 10 day samples, got %d", len(day))
	}
}

func TestQueryWeek(t *testing.T) {
	rrd, _ := New("")

	now := time.Now()
	for i := 0; i < 5; i++ {
		rrd.mu.Lock()
		rrd.week.push(now.Add(time.Duration(i)*time.Minute), int64(i*1000))
		rrd.mu.Unlock()
	}

	week := rrd.QueryWeek()
	if len(week) != 5 {
		t.Errorf("Expected 5 week samples, got %d", len(week))
	}
}

func TestQueryMonth(t *testing.T) {
	rrd, _ := New("")

	now := time.Now()
	for i := 0; i < 3; i++ {
		rrd.mu.Lock()
		rrd.mon.push(now.Add(time.Duration(i)*time.Hour), int64(i*10000))
		rrd.mu.Unlock()
	}

	month := rrd.QueryMonth()
	if len(month) != 3 {
		t.Errorf("Expected 3 month samples, got %d", len(month))
	}
}

func TestPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	persistPath := filepath.Join(tmpDir, "test.rrd")

	rrd1, err := New(persistPath)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	now := time.Now()
	for i := 0; i < 10; i++ {
		rrd1.mu.Lock()
		rrd1.day.push(now.Add(time.Duration(i)*time.Minute), int64(i*100))
		rrd1.mu.Unlock()
	}

	if err := rrd1.save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	rrd2, err := New(persistPath)
	if err != nil {
		t.Fatalf("New from persist failed: %v", err)
	}

	day2 := rrd2.QueryDay()
	if len(day2) != 10 {
		t.Errorf("Expected 10 samples after reload, got %d", len(day2))
	}

	// Samples are now from most recent to oldest
	if day2[0].Value != 900 {
		t.Errorf("Expected first (most recent) value 900, got %d", day2[0].Value)
	}
}

func TestPersistenceNoFile(t *testing.T) {
	rrd, err := New("")
	if err != nil {
		t.Fatalf("New with empty path failed: %v", err)
	}

	rrd.Record(100)
	rrd.Record(200)

	day := rrd.QueryDay()
	if len(day) != 2 {
		t.Errorf("Expected 2 samples, got %d", len(day))
	}
}

func TestScheduler(t *testing.T) {
	rrd, _ := New("")

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		rrd.StartScheduler(ctx, func() (int64, error) {
			return 1000, nil
		})
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Scheduler did not stop")
	}
}

func TestAlignedTicker(t *testing.T) {
	start := time.Now()
	ticker := alignedTicker(100 * time.Millisecond)
	defer ticker.Stop()

	select {
	case <-ticker.C:
		elapsed := time.Since(start)
		if elapsed > 200*time.Millisecond {
			t.Errorf("Ticker fired too late: %v", elapsed)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Ticker did not fire")
	}
}

func TestTimeUntilNext(t *testing.T) {
	d := 2 * time.Minute
	duration := timeUntilNext(d)

	if duration < 0 || duration > d {
		t.Errorf("Duration out of range: %v", duration)
	}
}
