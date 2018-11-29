package pktconn

import (
	"sync"
	"time"
)

type Ticker struct {
	sync.Mutex
	chans map[chan time.Time]struct{}
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

func NewTicker(interval time.Duration) *Ticker {
	t := &Ticker{
		chans: make(map[chan time.Time]struct{}),
	}
	go func() {
		for {
			time.Sleep(interval)
			now := time.Now()
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
