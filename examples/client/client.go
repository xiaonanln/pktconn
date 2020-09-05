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
	recvChan := make(chan *pktconn.Packet, 100)
	err = pc.Recv(recvChan)
	if err != nil {
		panic(err)
	}

	recvPacket := <-recvChan
	fmt.Printf("recv packet: %d\n", recvPacket.GetPayloadLen())
}
