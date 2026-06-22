package buffer

import (
	"sync"
	"sync/atomic"
	"wams-dashboard/internal/models"
	"wams-dashboard/internal/watchdog"
)

type LockFreeRingBuffer struct {
	capacity  uint32
	mask      uint32
	data      []*models.PhasorMeasurement
	head      uint32
	tail      uint32
	size      uint32
	pmuLatest map[string]*models.PhasorMeasurement
	watchdog  *watchdog.BufferWatchdog
	mu        sync.RWMutex
}

func NewLockFreeRingBuffer(capacity int) *LockFreeRingBuffer {
	actualCap := nextPowerOfTwo(uint32(capacity))
	rb := &LockFreeRingBuffer{
		capacity:  actualCap,
		mask:      actualCap - 1,
		data:      make([]*models.PhasorMeasurement, actualCap),
		head:      0,
		tail:      0,
		size:      0,
		pmuLatest: make(map[string]*models.PhasorMeasurement),
	}

	wd := watchdog.NewBufferWatchdog("phasor-buffer", int(actualCap), 100000)
	rb.watchdog = wd

	wd.SetBufferSizeGetter(func() int {
		return int(atomic.LoadUint32(&rb.size))
	})

	watchdog.GetWatchdogManager().Register(wd)

	return rb
}

func nextPowerOfTwo(n uint32) uint32 {
	if n == 0 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n++
	return n
}

func (rb *LockFreeRingBuffer) Push(pm *models.PhasorMeasurement) {
	currentSize := int(atomic.LoadUint32(&rb.size))
	if rb.watchdog != nil && rb.watchdog.ShouldDropPacket(currentSize) {
		return
	}

	rb.mu.Lock()
	rb.pmuLatest[pm.PMUID] = pm
	rb.mu.Unlock()

	for {
		head := atomic.LoadUint32(&rb.head)
		tail := atomic.LoadUint32(&rb.tail)
		size := atomic.LoadUint32(&rb.size)

		if size >= rb.capacity {
			newTail := (tail + 1) & rb.mask
			if atomic.CompareAndSwapUint32(&rb.tail, tail, newTail) {
				atomic.AddUint32(&rb.size, ^uint32(0))
				continue
			}
			continue
		}

		idx := head & rb.mask
		rb.data[idx] = pm

		newHead := (head + 1) & rb.mask
		if atomic.CompareAndSwapUint32(&rb.head, head, newHead) {
			atomic.AddUint32(&rb.size, 1)
			return
		}
	}
}

func (rb *LockFreeRingBuffer) Close() {
	if rb.watchdog != nil {
		watchdog.GetWatchdogManager().Unregister(rb.watchdog.Name())
	}
}

func (rb *LockFreeRingBuffer) Pop() *models.PhasorMeasurement {
	for {
		tail := atomic.LoadUint32(&rb.tail)
		size := atomic.LoadUint32(&rb.size)

		if size == 0 {
			return nil
		}

		idx := tail & rb.mask
		pm := rb.data[idx]
		if pm == nil {
			return nil
		}

		newTail := (tail + 1) & rb.mask
		if atomic.CompareAndSwapUint32(&rb.tail, tail, newTail) {
			atomic.AddUint32(&rb.size, ^uint32(0))
			return pm
		}
	}
}

func (rb *LockFreeRingBuffer) GetAll() []*models.PhasorMeasurement {
	tail := atomic.LoadUint32(&rb.tail)
	size := atomic.LoadUint32(&rb.size)

	if size == 0 {
		return []*models.PhasorMeasurement{}
	}

	result := make([]*models.PhasorMeasurement, 0, size)
	for i := uint32(0); i < size; i++ {
		idx := (tail + i) & rb.mask
		if rb.data[idx] != nil {
			result = append(result, rb.data[idx])
		}
	}
	return result
}

func (rb *LockFreeRingBuffer) GetLatest(limit int) []*models.PhasorMeasurement {
	head := atomic.LoadUint32(&rb.head)
	size := atomic.LoadUint32(&rb.size)

	count := uint32(limit)
	if count > size {
		count = size
	}

	result := make([]*models.PhasorMeasurement, 0, count)
	for i := uint32(0); i < count; i++ {
		idx := (head - 1 - i) & rb.mask
		if rb.data[idx] != nil {
			result = append([]*models.PhasorMeasurement{rb.data[idx]}, result...)
		}
	}
	return result
}

func (rb *LockFreeRingBuffer) GetLatestByPMU(pmuID string) *models.PhasorMeasurement {
	return rb.pmuLatest[pmuID]
}

func (rb *LockFreeRingBuffer) GetAllLatestByPMU() map[string]*models.PhasorMeasurement {
	result := make(map[string]*models.PhasorMeasurement)
	for k, v := range rb.pmuLatest {
		result[k] = v
	}
	return result
}

func (rb *LockFreeRingBuffer) Size() int {
	return int(atomic.LoadUint32(&rb.size))
}

func (rb *LockFreeRingBuffer) Capacity() int {
	return int(rb.capacity)
}
