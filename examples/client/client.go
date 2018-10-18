package main

import (
	"fmt"
	"net"

	"github.com/xiaonanln/go-packetconn"
)

func main() {
	conn, err := net.Dial("tcp", "localhost:14572")
	if err != nil {
		panic(err)
	}

	pc := packetconn.NewPacketConn(conn)
	defer pc.Close()

	packet := packetconn.NewPacket()
	payload := make([]byte, 1024)
	packet.AppendBytes(payload)

	pc.Send(packet)
	recvPacket := <-pc.Recv
	fmt.Printf("recv packet: %d\n", recvPacket.GetPayloadLen())
}
