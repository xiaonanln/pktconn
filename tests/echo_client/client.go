package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"sync"
	"time"

	packetconn "github.com/xiaonanln/go-packetconn"
)

const (
	port               = 14572
	perfClientCount    = 7000
	perfPayloadSizeMin = 0
	perfPayloadSizeMax = 2048
)

var (
	serverAddr = ""
)

func main() {
	serverAddr = os.Args[1]
	log.Printf("Server addr: %s", serverAddr)
	var done sync.WaitGroup
	done.Add(perfClientCount)
	var allConnected sync.WaitGroup
	allConnected.Add(perfClientCount)
	startSendRecv := make(chan int, perfClientCount)

	for i := 0; i < perfClientCount; i++ {
		client := &testPacketClient{}
		go client.routine(&done, &allConnected, startSendRecv)
	}
	log.Printf("wait for all clients to connected")
	allConnected.Wait()
	log.Printf("start %d send/recv ...", perfClientCount)
	for i := 0; i < perfClientCount; i++ {
		startSendRecv <- 1
	}

	done.Wait()
}

type testPacketClient struct {
}

func (c *testPacketClient) routine(done, allConnected *sync.WaitGroup, startSendRecv chan int) {
	defer done.Done()

restart:
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", serverAddr, port))
	if err != nil {
		log.Printf("connect error: %s", err)
		time.Sleep(time.Second)
		goto restart
	}

	cfg := packetconn.DefaultConfig()
	cfg.CrcChecksum = false
	pc := packetconn.NewPacketConnWithConfig(context.TODO(), conn, cfg)
	defer pc.Close()

	allConnected.Done()

	payload := make([]byte, perfPayloadSizeMin+rand.Intn(perfPayloadSizeMax-perfPayloadSizeMin+1))
	packet := packetconn.NewPacket()
	packet.WriteBytes(payload)

	<-startSendRecv

	for {
		pc.Send(packet)
		<-pc.Recv()
	}
}
