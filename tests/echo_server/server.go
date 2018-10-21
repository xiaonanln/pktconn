package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime"
	"sync/atomic"
	"time"

	_ "net/http/pprof"

	packetconn "github.com/xiaonanln/go-packetconn"
)

const (
	port = 14572
)

type testPacketServer struct {
	handlePacketCount      uint64
	totalHandlePacketCount uint64
	startupTime            time.Time
}

// ServeTCP serves on specified address as TCP server
func (ts *testPacketServer) serve(listenAddr string) error {
	ln, err := net.Listen("tcp", listenAddr)
	log.Printf("Listening on TCP: %s ...", listenAddr)

	if err != nil {
		return err
	}

	defer ln.Close()

	ts.startupTime = time.Now()
	go func() {
		for {
			count := atomic.SwapUint64(&ts.handlePacketCount, 0)
			totalCount := atomic.AddUint64(&ts.totalHandlePacketCount, count)
			elapsedTime := (time.Now().Sub(ts.startupTime)) / time.Second
			if elapsedTime == 0 {
				elapsedTime = 1
			}

			log.Printf("CUR %d, AVG %d/s", count, totalCount/uint64(elapsedTime))
			time.Sleep(time.Second)
		}
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if packetconn.IsTemporary(err) {
				runtime.Gosched()
				continue
			} else {
				return err
			}
		}

		log.Printf("%s connected", conn.RemoteAddr())
		go func() {
			cfg := packetconn.DefaultConfig()
			cfg.FlushInterval = time.Millisecond * 100
			cfg.CrcChecksum = false
			cfg.WriteBufferSize = 8192
			cfg.ReadBufferSize = 8192
			pc := packetconn.NewPacketConnWithConfig(context.TODO(), conn, cfg)

			for pkt := range pc.Recv() {
				pc.Send(pkt)
				pkt.Release()
				atomic.AddUint64(&ts.handlePacketCount, 1)
			}
		}()
	}
}

func main() {
	runtime.GOMAXPROCS(1)
	go func() {
		log.Println(http.ListenAndServe("0.0.0.0:6060", nil))
	}()
	var server testPacketServer
	server.serve(fmt.Sprintf("0.0.0.0:%d", port))
}
