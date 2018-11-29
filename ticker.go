package pktconn

import (
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

type Ticker struct {
	sync.Mutex
	chans map[chan time.Time]struct{}
	now   unsafe.Pointer
}

func (t *Ticker) AddChan(ch chan time.Time) {
	t.Lock()
	t.chans[ch] = struct{}{}
	t.Unlock()
}

func (t *Ticker) RemoveChan(ch chan time.Time) {
	t.Lock()
	delete(t.chans, ch)
	t.Unlock()
}

func (t *Ticker) Now() time.Time {
	return *(*time.Time)(atomic.LoadPointer(&t.now))
}

func NewTicker(interval time.Duration) *Ticker {
	now := time.Now()
	t := &Ticker{
		chans: make(map[chan time.Time]struct{}),
	}
	atomic.StorePointer(&t.now, unsafe.Pointer(&now))

	go func() {
		for {
			time.Sleep(interval)
			now := time.Now()
			atomic.StorePointer(&t.now, unsafe.Pointer(&now))
			t.Lock()
			for ch := range t.chans {
				select {
				case ch <- now:
				default:
				}
			}
			t.Unlock()
		}
	}()
	return t
}
