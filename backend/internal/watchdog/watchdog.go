package watchdog

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

type AlertLevel int

const (
	AlertLevelNormal AlertLevel = iota
	AlertLevelWarning
	AlertLevelCritical
	AlertLevelEmergency
)

type WatchdogStats struct {
	BufferWatermarkHigh   uint64
	BufferWatermarkLow    uint64
	BufferWatermarkCritical uint64
	TotalDroppedPackets uint64
	TotalProcessedPackets uint64
	AlertCountWarning   uint64
	AlertCountCritical  uint64
	AlertCountEmergency uint64
	MaxObservedRate  uint64
	AvgObservedRate uint64
}

type SlidingWindowCounter struct {
	windowSize time.Duration
	buckets    []uint64
	mu         sync.Mutex
}

func NewSlidingWindowCounter(windowSize time.Duration, bucketCount int) *SlidingWindowCounter {
	return &SlidingWindowCounter{
		windowSize: windowSize,
		buckets:    make([]uint64, bucketCount),
	}
}

func (sw *SlidingWindowCounter) Increment() {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	bucketDuration := int64(sw.windowSize) / int64(len(sw.buckets))
	idx := int((time.Now().UnixNano() % int64(sw.windowSize)) / bucketDuration)
	sw.buckets[idx]++
}

func (sw *SlidingWindowCounter) Sum() uint64 {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	var total uint64
	for _, v := range sw.buckets {
		total += v
	}
	return total
}

func (sw *SlidingWindowCounter) Reset() {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	for i := range sw.buckets {
		sw.buckets[i] = 0
	}
}

type BufferWatchdog struct {
	name             string
	bufferCapacity   int
	warningThreshold   int
	criticalThreshold  int
	emergencyThreshold int
	rateLimitPerSec    uint64
	bufferSizeGetter func() int

	watermarkHigh    atomic.Uint64
	watermarkLow     atomic.Uint64
	watermarkCritical atomic.Uint64
	droppedPackets   atomic.Uint64
	processedPackets atomic.Uint64
	alertCountWarning  atomic.Uint64
	alertCountCritical atomic.Uint64
	alertCountEmergency atomic.Uint64
	maxObservedRate atomic.Uint64

	rateCounter *SlidingWindowCounter

	alertLevel atomic.Int32

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	alertCallbacks []func(AlertLevel, string)

	mu sync.Mutex
}

func NewBufferWatchdog(name string, bufferCapacity int, rateLimitPerSec uint64) *BufferWatchdog {
	ctx, cancel := context.WithCancel(context.Background())

	warningThreshold := int(float64(bufferCapacity) * 0.6)
	criticalThreshold := int(float64(bufferCapacity) * 0.8)
	emergencyThreshold := int(float64(bufferCapacity) * 0.95)

	if warningThreshold <= 0 {
		warningThreshold = bufferCapacity * 6 / 10
	}
	if criticalThreshold <= warningThreshold {
		criticalThreshold = bufferCapacity * 8 / 10
	}
	if emergencyThreshold <= criticalThreshold {
		emergencyThreshold = bufferCapacity * 95 / 100
	}

	return &BufferWatchdog{
		name:                name,
		bufferCapacity:      bufferCapacity,
		warningThreshold:  warningThreshold,
		criticalThreshold: criticalThreshold,
		emergencyThreshold: emergencyThreshold,
		rateLimitPerSec:   rateLimitPerSec,
		rateCounter:      NewSlidingWindowCounter(1*time.Second, 10),
		ctx:              ctx,
		cancel:           cancel,
	}
}

func (wd *BufferWatchdog) OnAlert(callback func(AlertLevel, string)) {
	wd.mu.Lock()
	defer wd.mu.Unlock()
	wd.alertCallbacks = append(wd.alertCallbacks, callback)
}

func (wd *BufferWatchdog) SetBufferSizeGetter(getter func() int) {
	wd.mu.Lock()
	defer wd.mu.Unlock()
	wd.bufferSizeGetter = getter
}

func (wd *BufferWatchdog) Start() {
	wd.wg.Add(1)
	go wd.monitorLoop()
	log.Printf("Buffer watchdog '%s' started (capacity=%d, rateLimit=%d/sec",
		wd.name, wd.bufferCapacity, wd.rateLimitPerSec)
}

func (wd *BufferWatchdog) Stop() {
	wd.cancel()
	wd.wg.Wait()
	log.Printf("Buffer watchdog '%s' stopped", wd.name)
}

func (wd *BufferWatchdog) monitorLoop() {
	defer wd.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	statsTicker := time.NewTicker(5 * time.Second)
	defer statsTicker.Stop()

	for {
		select {
		case <-wd.ctx.Done():
			return
		case <-ticker.C:
			wd.checkStatus()
		case <-statsTicker.C:
			wd.reportStats()
		}
	}
}

func (wd *BufferWatchdog) checkStatus() {
	currentSize := wd.GetBufferSize()
	currentRate := wd.rateCounter.Sum()

	if currentSize > int(wd.watermarkHigh.Load()) {
		wd.watermarkHigh.Store(uint64(currentSize))
	}
	if currentSize < int(wd.watermarkLow.Load()) || wd.watermarkLow.Load() == 0 {
		wd.watermarkLow.Store(uint64(currentSize))
	}
	if currentSize > int(wd.watermarkCritical.Load()) {
		wd.watermarkCritical.Store(uint64(currentSize))
	}

	if currentRate > wd.maxObservedRate.Load() {
		wd.maxObservedRate.Store(currentRate)
	}

	var newLevel AlertLevel
	switch {
	case currentSize >= wd.emergencyThreshold || currentRate > wd.rateLimitPerSec*10:
		newLevel = AlertLevelEmergency
	case currentSize >= wd.criticalThreshold || currentRate > wd.rateLimitPerSec*5:
		newLevel = AlertLevelCritical
	case currentSize >= wd.warningThreshold || currentRate > wd.rateLimitPerSec*2:
		newLevel = AlertLevelWarning
	default:
		newLevel = AlertLevelNormal
	}

	oldLevel := AlertLevel(wd.alertLevel.Load())
	if newLevel != oldLevel {
		wd.alertLevel.Store(int32(newLevel))
		wd.handleAlert(newLevel, currentSize, currentRate)
	}

	wd.rateCounter.Reset()
}

func (wd *BufferWatchdog) handleAlert(level AlertLevel, bufferSize int, rate uint64) {
	var msg string
	switch level {
	case AlertLevelNormal:
		msg = "System recovered to normal operation"
		wd.logAlert(level, msg)
	case AlertLevelWarning:
		wd.alertCountWarning.Add(1)
		msg = "High buffer usage warning"
		wd.logAlert(level, msg)
	case AlertLevelCritical:
		wd.alertCountCritical.Add(1)
		msg = "Critical buffer usage - traffic shaping activated"
		wd.logAlert(level, msg)
	case AlertLevelEmergency:
		wd.alertCountEmergency.Add(1)
		msg = "EMERGENCY: Buffer overflow risk - aggressive dropping"
		wd.logAlert(level, msg)
	}

	wd.mu.Lock()
	callbacks := make([]func(AlertLevel, string), len(wd.alertCallbacks))
	copy(callbacks, wd.alertCallbacks)
	wd.mu.Unlock()

	for _, cb := range callbacks {
		func(cb func(AlertLevel, string)) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("PANIC in alert callback: %v", r)
				}
			}()
			cb(level, msg)
		}(cb)
	}
}

func (wd *BufferWatchdog) logAlert(level AlertLevel, msg string) {
	currentSize := wd.GetBufferSize()
	rate := wd.rateCounter.Sum()

	levelStr := [...]string{"NORMAL", "WARNING", "CRITICAL", "EMERGENCY"}[level]

	log.Printf("[WATCHDOG:%s] %s - buffer=%d/%d (%.1f%%), rate=%d/sec, limit=%d/sec",
		wd.name,
		levelStr,
		currentSize,
		wd.bufferCapacity,
		float64(currentSize)/float64(wd.bufferCapacity)*100,
		rate,
		wd.rateLimitPerSec,
	)
}

func (wd *BufferWatchdog) reportStats() {
	stats := wd.GetStats()
	log.Printf("[WATCHDOG:%s] Stats - processed=%d, dropped=%d, watermark_high=%d, watermark_low=%d, alerts_warning=%d, alerts_critical=%d, alerts_emergency=%d, max_rate=%d/sec",
		wd.name,
		stats.TotalProcessedPackets,
		stats.TotalDroppedPackets,
		stats.BufferWatermarkHigh,
		stats.BufferWatermarkLow,
		stats.AlertCountWarning,
		stats.AlertCountCritical,
		stats.AlertCountEmergency,
		stats.MaxObservedRate,
	)
}

func (wd *BufferWatchdog) ShouldDropPacket(bufferSize int) bool {
	if wd.ctx.Err() != nil {
		return false
	}

	wd.rateCounter.Increment()
	wd.processedPackets.Add(1)

	level := AlertLevel(wd.alertLevel.Load())

	if level == AlertLevelEmergency {
		wd.droppedPackets.Add(1)
		return true
	}

	if level == AlertLevelCritical {
		if bufferSize > wd.criticalThreshold*11/10 {
			wd.droppedPackets.Add(1)
			return true
		}
	}

	currentRate := wd.rateCounter.Sum()
	if currentRate > wd.rateLimitPerSec*20 {
		wd.droppedPackets.Add(1)
		return true
	}

	return false
}

func (wd *BufferWatchdog) GetBufferSize() int {
	wd.mu.Lock()
	getter := wd.bufferSizeGetter
	wd.mu.Unlock()

	if getter != nil {
		return getter()
	}
	return 0
}

func (wd *BufferWatchdog) GetAlertLevel() AlertLevel {
	return AlertLevel(wd.alertLevel.Load())
}

func (wd *BufferWatchdog) Name() string {
	return wd.name
}

func (wd *BufferWatchdog) GetStats() WatchdogStats {
	return WatchdogStats{
		BufferWatermarkHigh:    wd.watermarkHigh.Load(),
		BufferWatermarkLow:     wd.watermarkLow.Load(),
		BufferWatermarkCritical: wd.watermarkCritical.Load(),
		TotalDroppedPackets:  wd.droppedPackets.Load(),
		TotalProcessedPackets:  wd.processedPackets.Load(),
		AlertCountWarning:   wd.alertCountWarning.Load(),
		AlertCountCritical:  wd.alertCountCritical.Load(),
		AlertCountEmergency: wd.alertCountEmergency.Load(),
		MaxObservedRate:   wd.maxObservedRate.Load(),
	}
}

type WatchdogManager struct {
	watchdogs map[string]*BufferWatchdog
	mu        sync.RWMutex
}

var (
	instance *WatchdogManager
	once     sync.Once
)

func GetWatchdogManager() *WatchdogManager {
	once.Do(func() {
		instance = &WatchdogManager{
			watchdogs: make(map[string]*BufferWatchdog),
		}
	})
	return instance
}

func (m *WatchdogManager) Register(watchdog *BufferWatchdog) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watchdogs[watchdog.name] = watchdog
	watchdog.Start()
}

func (m *WatchdogManager) Unregister(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if wd, exists := m.watchdogs[name]; exists {
		wd.Stop()
		delete(m.watchdogs, name)
	}
}

func (m *WatchdogManager) Get(name string) (*BufferWatchdog, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	wd, exists := m.watchdogs[name]
	return wd, exists
}

func (m *WatchdogManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, wd := range m.watchdogs {
		wd.Stop()
		delete(m.watchdogs, name)
	}
}

func (m *WatchdogManager) GetAllStats() map[string]WatchdogStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	stats := make(map[string]WatchdogStats)
	for name, wd := range m.watchdogs {
		stats[name] = wd.GetStats()
	}
	return stats
}

func (m *WatchdogManager) GetOverallAlertLevel() AlertLevel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	maxLevel := AlertLevelNormal
	for _, wd := range m.watchdogs {
		level := wd.GetAlertLevel()
		if level > maxLevel {
			maxLevel = level
		}
	}
	return maxLevel
}
