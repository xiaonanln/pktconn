package main

import (
	"fmt"
	"log"
	"net"
	"runtime"
	"sync/atomic"
	"time"

	packetconn "github.com/xiaonanln/go-packetconn"
)

const (
	port = 14572
)

type testPacketServer struct {
	handlePacketCount uint64
}

// ServeTCP serves on specified address as TCP server
func (ts *testPacketServer) serve(listenAddr string) error {
	ln, err := net.Listen("tcp", listenAddr)
	log.Printf("Listening on TCP: %s ...", listenAddr)

	if err != nil {
		return err
	}

	defer ln.Close()

	go func() {
		for {
			count := atomic.SwapUint64(&ts.handlePacketCount, 0)
			log.Printf("handling %d packets per second", count)
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
			pc := packetconn.NewPacketConn(conn)

			for pkt := range pc.Recv {
				pc.Send(pkt)
				pkt.Release()
				atomic.AddUint64(&ts.handlePacketCount, 1)
			}
		}()
	}
}

func main() {
	var server testPacketServer
	server.serve(fmt.Sprintf("localhost:%d", port))
}
