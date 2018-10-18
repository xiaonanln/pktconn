package tests

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	packetconn "github.com/xiaonanln/go-packetconn"
)

const (
	port       = 14572
	bufferSize = 8192 * 2
	//recvChanSize    = 1000
	//flushInterval   = time.Millisecond * 5
	noackCountLimit = 1000
	perfClientCount = 1000
	perfPayloadSize = 1024
	perfDuration    = time.Second * 5
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
		pc := packetconn.NewPacketConn(conn)

		for pkt := range pc.Recv {
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

	pconn := packetconn.NewPacketConnWithConfig(conn, cfg)

	for i := 0; i < 10; i++ {
		var PAYLOAD_LEN uint32 = uint32(rand.Intn(4096 + 1))
		t.Logf("Testing with payload %v", PAYLOAD_LEN)

		packet := packetconn.NewPacket()
		for j := uint32(0); j < PAYLOAD_LEN; j++ {
			packet.AppendByte(byte(rand.Intn(256)))
		}
		if packet.GetPayloadLen() != PAYLOAD_LEN {
			t.Errorf("payload should be %d, but is %d", PAYLOAD_LEN, packet.GetPayloadLen())
		}
		pconn.Send(packet)
		if recvPacket, ok := <-pconn.Recv; ok {
			if packet.GetPayloadLen() != recvPacket.GetPayloadLen() {
				t.Errorf("send packet len %d, but recv len %d", packet.GetPayloadLen(), recvPacket.GetPayloadLen())
			}
			for i := uint32(0); i < packet.GetPayloadLen(); i++ {
				if packet.Payload()[i] != recvPacket.Payload()[i] {
					t.Errorf("send packet and recv packet mismatch on byte index %d", i)
				}
			}
		} else {
			t.Fatalf("can not recv packet")
		}
	}
}

type testPacketClient struct {
}

func (c *testPacketClient) routine(t *testing.T, done, allConnected *sync.WaitGroup, startSendRecv chan int) {
restart:
	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		t.Errorf("connect error: %s", err)
		time.Sleep(time.Second)
		goto restart
	}

	pc := packetconn.NewPacketConn(conn)

	allConnected.Done()

	payload := make([]byte, perfPayloadSize)
	packet := packetconn.NewPacket()
	packet.AppendBytes(payload)

	<-startSendRecv
	stopTime := time.Now().Add(perfDuration)
	noackCount := 0

	for time.Now().Before(stopTime) {
		pc.Send(packet)
		noackCount += 1

		for noackCount > noackCountLimit {
			<-pc.Recv
			noackCount -= 1
		}
	}

	for noackCount > 0 {
		<-pc.Recv
		noackCount -= 1
	}
	pc.Close()
	done.Done()
}

func TestPacketConnPerf(t *testing.T) {
	w, err := os.OpenFile("test.pprof", os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	pprof.StartCPUProfile(w)
	defer pprof.StopCPUProfile()
	var done sync.WaitGroup
	done.Add(perfClientCount)
	var allConnected sync.WaitGroup
	allConnected.Add(perfClientCount)
	startSendRecv := make(chan int, perfClientCount)

	for i := 0; i < perfClientCount; i++ {
		client := &testPacketClient{}
		go client.routine(t, &done, &allConnected, startSendRecv)
	}
	t.Log("wait for all clients to connected")
	allConnected.Wait()
	t.Log("start send/recv ...")
	for i := 0; i < perfClientCount; i++ {
		startSendRecv <- 1
	}

	done.Wait()

	w, err = os.OpenFile("heap.pprof", os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	pprof.WriteHeapProfile(w)
}
