package ratelimit

import (
	"fmt"
	"math"
	"sync"
	"time"
)

const (
	KiloBytes = 1 << 10
	MegaBytes = 1 << 20
	GigaBytes = 1 << 30
)

type Bucket struct {
	// start time
	start time.Time

	// max number of token
	capacity int64

	// number of token to refill at each interval
	quantum int64

	// refill interval
	interval time.Duration

	// current number of token at current tick
	tokens int64

	// current tick
	tick int64

	// protect all members
	mutex sync.Mutex
}

func nextQuantum(quantum int64) int64 {
	nextQuantum := quantum * 11 / 10
	if nextQuantum == quantum {
		nextQuantum++
	}
	return nextQuantum
}

func findQuantumAndInterval(rate, margin float64) (int64, time.Duration) {
	for quantum := int64(1); quantum < 1<<50; quantum = nextQuantum(quantum) {
		interval := time.Duration(1e9 * float64(quantum) / rate)
		if interval <= 0 {
			continue
		}
		actualRate := 1e9 * float64(quantum) / float64(interval)
		if diff := math.Abs(actualRate - rate); diff/rate <= margin {
			return quantum, interval
		}
	}
	panic(fmt.Errorf("ratelimit: cannot find a suitable quantum and interval for rate %v within %v margin", rate, margin))
}

func NewBucketWithQuantumAndInterval(quantum int64, interval time.Duration, capacity int64) *Bucket {
	self := &Bucket{}
	self.ResetQuantumAndInterval(quantum, interval, capacity)
	return self
}

func NewBucketWithRate(rate float64, capacity int64) *Bucket {
	self := &Bucket{}
	self.ResetRate(rate, capacity)
	return self
}

func (self *Bucket) ResetQuantumAndInterval(quantum int64, interval time.Duration, capacity int64) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.start = time.Now()

	self.quantum = quantum
	self.interval = interval
	self.capacity = capacity

	self.tick = 0
	self.tokens = capacity
}

func (self *Bucket) ResetRate(rate float64, capacity int64) {
	quantum, interval := findQuantumAndInterval(rate, 0.01)
	self.ResetQuantumAndInterval(quantum, interval, capacity)
}

// Get capacity of the bucket.
func (self *Bucket) Capacity() int64 {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.capacity
}

func (self *Bucket) Rate() float64 {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return 1e9 * float64(self.quantum) / float64(self.interval)
}

// Wait for availability.
func (self *Bucket) Wait(count int64) {
	if duration := self.Take(count); duration > 0 {
		time.Sleep(duration)
	}
}

// Take from bucket immediately.
func (self *Bucket) Take(count int64) time.Duration {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.take(time.Now(), count)
}

func (self *Bucket) take(now time.Time, count int64) time.Duration {
	// assume under mutex
	if count <= 0 {
		return 0
	}

	tick := self.currentTick(now)
	self.adjust(tick)
	available := self.tokens - count
	if available >= 0 {
		self.tokens = available
		return 0
	}

	endTick := tick + (-available+self.quantum-1)/self.quantum
	endTime := self.start.Add(time.Duration(endTick) * self.interval)
	waitTime := endTime.Sub(now)
	self.tokens = available
	return waitTime
}

func (self *Bucket) TakeAvailable(count int64) int64 {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.takeAvailable(time.Now(), count)
}

func (self *Bucket) takeAvailable(now time.Time, count int64) int64 {
	// assume under mutex
	if count <= 0 {
		return 0
	}
	self.adjust(self.currentTick(now))
	if self.tokens <= 0 {
		return 0
	}
	if count > self.tokens {
		count = self.tokens
	}
	self.tokens -= count
	return count
}

func (self *Bucket) Available() int64 {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.available(time.Now())
}

func (self *Bucket) available(now time.Time) int64 {
	// assume under mutex
	self.adjust(self.currentTick(now))
	return self.tokens
}

func (self *Bucket) currentTick(now time.Time) int64 {
	// assume under mutex
	return int64(now.Sub(self.start) / self.interval)
}

func (self *Bucket) adjust(tick int64) {
	// assume under mutex
	previousTick := self.tick
	self.tick = tick
	if self.tokens >= self.capacity {
		return
	}
	self.tokens += (tick - previousTick) * self.quantum
	if self.tokens > self.capacity {
		self.tokens = self.capacity
	}
}
