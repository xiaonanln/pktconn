package main

import (
	"context"
	"fmt"
	"net"

	"github.com/xiaonanln/pktconn"
)

func main() {
	conn, err := net.Dial("tcp", "localhost:14572")
	if err != nil {
		panic(err)
	}

	pc := pktconn.NewPacketConn(context.TODO(), conn)
	defer pc.Close()

	packet := pktconn.NewPacket()
	payload := make([]byte, 1024)
	packet.WriteBytes(payload)

	pc.Send(packet)
	recvPacket := <-pc.Recv()
	fmt.Printf("recv packet: %d\n", recvPacket.GetPayloadLen())
}
