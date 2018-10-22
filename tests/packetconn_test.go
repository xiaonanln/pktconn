package tests

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaonanln/go-packetconn"
)

const (
	port       = 14572
	bufferSize = 8192 * 2
)

type testPacketServer struct {
	handlePacketCount uint64
}

// ServeTCP serves on specified address as TCP server
func (ts *testPacketServer) serve(listenAddr string, serverReady chan bool) error {
	ln, err := net.Listen("tcp", listenAddr)
	log.Printf("Listening on TCP: %s ...", listenAddr)

	if err != nil {
		return err
	}

	defer ln.Close()
	serverReady <- true

	go func() {
		for {
			count := atomic.SwapUint64(&ts.handlePacketCount, 0)
			println(count, "packets per second")
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
		ts.serveTCPConn(conn)
	}
}

func (ts *testPacketServer) serveTCPConn(conn net.Conn) {
	go func() {
		pc := packetconn.NewPacketConn(context.TODO(), conn)

		for pkt := range pc.Recv() {
			pc.Send(pkt)
			pkt.Release()
			atomic.AddUint64(&ts.handlePacketCount, 1)
		}
	}()
}

func init() {
	var server testPacketServer
	serverReady := make(chan bool)
	go server.serve(fmt.Sprintf("localhost:%d", port), serverReady)
	<-serverReady
}

func TestPacketConnWithBuffer(t *testing.T) {
	testPacketConnRS(t, true)
}

func TestPacketConnWithoutBuffer(t *testing.T) {
	testPacketConnRS(t, false)
}

func testPacketConnRS(t *testing.T, useBufferedConn bool) {
	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		t.Errorf("connect error: %s", err)
	}

	cfg := packetconn.DefaultConfig()

	if useBufferedConn {
		cfg.ReadBufferSize = bufferSize
		cfg.WriteBufferSize = bufferSize
	} else {
		cfg.ReadBufferSize = 0
		cfg.WriteBufferSize = 0
	}
	cfg.Tag = t

	pconn := packetconn.NewPacketConnWithConfig(context.TODO(), conn, cfg)

	for i := 0; i < 10; i++ {
		var PAYLOAD_LEN uint32 = uint32(rand.Intn(4096 + 1))
		t.Logf("Testing with payload %v", PAYLOAD_LEN)

		packet := packetconn.NewPacket()
		for j := uint32(0); j < PAYLOAD_LEN; j++ {
			packet.WriteOneByte(byte(rand.Intn(256)))
		}
		if packet.GetPayloadLen() != PAYLOAD_LEN {
			t.Errorf("payload should be %d, but is %d", PAYLOAD_LEN, packet.GetPayloadLen())
		}
		pconn.Send(packet)
		if recvPacket, ok := <-pconn.Recv(); ok {
			if packet.GetPayloadLen() != recvPacket.GetPayloadLen() {
				t.Errorf("send packet len %d, but recv len %d", packet.GetPayloadLen(), recvPacket.GetPayloadLen())
			}
			for i := uint32(0); i < packet.GetPayloadLen(); i++ {
				if packet.Payload()[i] != recvPacket.Payload()[i] {
					t.Errorf("send packet and recv packet mismatch on byte index %d", i)
				}
			}
		} else {
			t.Fatalf("can not recv packet: %v", pconn.Err())
		}
	}
}
