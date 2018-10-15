package packetconn

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaonanln/go-simplelogger"
)

const (
	port            = 14572
	bufferSize      = 8192 * 2
	flushInterval   = time.Millisecond * 5
	noackCountLimit = 10000
	perfClientCount = 1000
	perfPayloadSize = 128
	perfDuration    = time.Second * 10
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
			if IsTimeout(err) {
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
		pc := NewPacketConn(NewBufferedConn(conn, bufferSize, bufferSize))

		go func() {
			for {
				err := pc.Flush()
				if err != nil && !IsTemporary(err) {
					log.Printf("packet conn quit")
					break
				}

				time.Sleep(flushInterval)
			}
		}()

		for {
			pkt, err := pc.Recv()

			if pkt != nil {
				//log.Printf("Recv packet with payload %d", pkt.GetPayloadLen())
				pc.SendPacket(pkt)
				pkt.Release()
				atomic.AddUint64(&ts.handlePacketCount, 1)
			}

			if err != nil {
				if IsTemporary(err) {
					continue
				} else {
					break
				}
			}
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

	if useBufferedConn {
		conn = NewBufferedConn(conn, bufferSize, bufferSize)
	}
	pconn := NewPacketConn(conn)

	for i := 0; i < 10; i++ {
		var PAYLOAD_LEN uint32 = uint32(rand.Intn(4096 + 1))
		simplelogger.Infof("Testing with payload %v", PAYLOAD_LEN)

		packet := pconn.NewPacket()
		for j := uint32(0); j < PAYLOAD_LEN; j++ {
			packet.AppendByte(byte(rand.Intn(256)))
		}
		if packet.GetPayloadLen() != PAYLOAD_LEN {
			t.Errorf("payload should be %d, but is %d", PAYLOAD_LEN, packet.GetPayloadLen())
		}
		pconn.SendPacket(packet)
		pconn.Flush()
		var recvPacket *Packet
		var err error
		for {
			recvPacket, err = pconn.Recv()
			if err != nil {
				if IsTemporary(err) {
					continue
				} else {
					t.Fatal(err)
				}
			} else {
				break
			}
		}

		if packet.GetPayloadLen() != recvPacket.GetPayloadLen() {
			t.Errorf("send packet len %d, but recv len %d", packet.GetPayloadLen(), recvPacket.GetPayloadLen())
		}
		for i := uint32(0); i < packet.GetPayloadLen(); i++ {
			if packet.Payload()[i] != recvPacket.Payload()[i] {
				t.Errorf("send packet and recv packet mismatch on byte index %d", i)
			}
		}
	}
}

type testPacketClient struct {
}

func (c *testPacketClient) routine(t *testing.T, done, allConnected *sync.WaitGroup, startSendRecv chan int) {
	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		t.Errorf("connect error: %s", err)
	}

	pc := NewPacketConn(NewBufferedConn(conn, bufferSize, bufferSize))

	allConnected.Done()

	payload := make([]byte, perfPayloadSize)
	packet := NewPacket()
	packet.AppendBytes(payload)

	<-startSendRecv
	stopTime := time.Now().Add(perfDuration)
	noackCount := 0

	for time.Now().Before(stopTime) {
		pc.SendPacket(packet)
		pc.Flush()
		noackCount += 1

		for noackCount > noackCountLimit {
			for {
				_, err := pc.Recv()
				if err != nil {
					if IsTemporary(err) {
						continue
					} else {
						t.Fatalf("recv failed: %s", err)
					}
				} else {
					noackCount -= 1
					break
				}
			}
		}
	}

	for noackCount > 0 {
		for {
			_, err := pc.Recv()
			if err != nil {
				if IsTemporary(err) {
					continue
				} else {
					t.Fatalf("recv failed: %s", err)
				}
			} else {
				noackCount -= 1
				break
			}
		}
	}
	pc.Close()
	done.Done()
}

func TestPacketConnPerf(t *testing.T) {
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
}
