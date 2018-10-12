package packetconn

import (
	"fmt"
	"math/rand"
	"net"
	"testing"
	"time"

	simplelogger "github.com/xiaonanln/go-simplelogger"
)

type testEchoTcpServer struct {
}

func (ts *testEchoTcpServer) ServeTCPConn(conn net.Conn) {
	buf := make([]byte, 1024*1024, 1024*1024)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			WriteAll(conn, buf[:n])
		}

		if err != nil {
			if IsTimeoutError(err) {
				continue
			} else {
				simplelogger.Infof("read error: %s", err.Error())
				break
			}
		}
	}
}

const PORT = 14572

func init() {
	go func() {
		ServeTCP(fmt.Sprintf("localhost:%d", PORT), &testEchoTcpServer{})
	}()
	time.Sleep(time.Millisecond * 200)
}

func TestPacketConnection(t *testing.T) {

	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", PORT))
	if err != nil {
		t.Errorf("connect error: %s", err)
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
			recvPacket, err = pconn.RecvPacket()
			if err != nil {
				if IsTimeoutError(err) {
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
