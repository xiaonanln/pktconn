package packetconn

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"testing"
	"time"

	simplelogger "github.com/xiaonanln/go-simplelogger"
)

const (
	testPort       = 14572
	testBufferSize = 16384
)

type testPacketServer struct {
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
		pc := NewPacketConn(NewBufferedConn(conn, testBufferSize, testBufferSize))

		go func() {
			for {
				err := pc.Flush()
				if err != nil && !IsTemporary(err) {
					log.Printf("packet conn quit")
					break
				}

				time.Sleep(time.Millisecond * 5)
			}
		}()

		for {
			pkt, err := pc.Recv()

			if pkt != nil {
				//log.Printf("Recv packet with payload %d", pkt.GetPayloadLen())
				pc.SendPacket(pkt)
				pkt.Release()
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
	go server.serve(fmt.Sprintf("localhost:%d", testPort), serverReady)
	<-serverReady
}

func TestPacketConnWithBuffer(t *testing.T) {
	testPacketConnRS(t, true)
}

func TestPacketConnWithoutBuffer(t *testing.T) {
	testPacketConnRS(t, false)
}

func testPacketConnRS(t *testing.T, useBufferedConn bool) {
	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", testPort))
	if err != nil {
		t.Errorf("connect error: %s", err)
	}

	if useBufferedConn {
		conn = NewBufferedConn(conn, testBufferSize, testBufferSize)
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

func TestPacketConnPerf(t *testing.T) {
	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", testPort))
	if err != nil {
		t.Errorf("connect error: %s", err)
	}

	pc := NewPacketConn(NewBufferedConn(conn, testBufferSize, testBufferSize))

	go func() {
		counter := 0
		for {
			_, err := pc.Recv()
			if err != nil {
				if IsTemporary(err) {
					continue
				} else {
					break
				}
			}
			counter += 1
		}

		t.Logf("send/recv %d packets in 10s", counter)
	}()

	go func() {
		for {
			err := pc.Flush()
			if err != nil && !IsTemporary(err) {
				log.Printf("packet conn quit")
				break
			}

			time.Sleep(time.Millisecond * 5)
		}
	}()
	payload := make([]byte, 1024)
	packet := NewPacket()
	packet.AppendBytes(payload)

	stopTime := time.Now().Add(time.Second * 10)
	for time.Now().Before(stopTime) {
		pc.SendPacket(packet)
	}
	pc.Close()
}
