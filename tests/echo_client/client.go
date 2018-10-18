package main

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"
    "math/rand"

	packetconn "github.com/xiaonanln/go-packetconn"
)

const (
	port            = 14572
	noackCountLimit = 1000
	perfClientCount = 3000
	perfPayloadSizeMin = 0
	perfPayloadSizeMax = 2048
)

func main() {
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
	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		log.Printf("connect error: %s", err)
		time.Sleep(time.Second)
		goto restart
	}

	pc := packetconn.NewPacketConn(conn)
	defer pc.Close()

	allConnected.Done()

	payload := make([]byte, perfPayloadSizeMin + rand.Intn(perfPayloadSizeMax - perfPayloadSizeMin+1))
	packet := packetconn.NewPacket()
	packet.AppendBytes(payload)

	<-startSendRecv
	noackCount := 0

	for {
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
}
