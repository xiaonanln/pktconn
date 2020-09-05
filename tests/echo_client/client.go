package main

import (
	"context"
	"fmt"
	"github.com/xiaonanln/netconnutil"
	"log"
	"math/rand"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaonanln/pktconn"
)

const (
	port                     = 14572
	perfClientCount          = 100
	perfPayloadSizeMin       = 0
	perfPayloadSizeMax       = 2048
	perfEchoCounterPerClient = 1000
)

var (
	serverAddr   = ""
	totalCounter int32
)

func main() {
	serverAddr = os.Args[1]
	log.Printf("Server addr: %s", serverAddr)
	var done sync.WaitGroup
	done.Add(perfClientCount)
	var allConnected sync.WaitGroup
	allConnected.Add(perfClientCount)
	startSendRecv := make(chan int)

	for i := 0; i < perfClientCount; i++ {
		client := &testPacketClient{}
		go client.routine(&done, &allConnected, startSendRecv)
	}
	log.Printf("wait for all clients to connected")
	allConnected.Wait()
	log.Printf("start %d send/recv ...", perfClientCount)

	close(startSendRecv)

	t0 := time.Now()

	done.Wait()

	if totalCounter != perfClientCount*perfEchoCounterPerClient {
		panic(fmt.Errorf("total counter should be %d, but is %d", perfClientCount*perfEchoCounterPerClient, totalCounter))
	}

	dt := time.Since(t0)
	fmt.Printf("%d %v %d\n", totalCounter, dt, int(float64(totalCounter)/dt.Seconds()))
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

	conn = netconnutil.NewBufferedConn(conn, 8192, 8192)

	cfg := pktconn.DefaultConfig()
	cfg.CrcChecksum = false
	cfg.FlushDelay = time.Millisecond * 1
	cfg.MaxFlushDelay = time.Millisecond * 10
	pc := pktconn.NewPacketConnWithConfig(context.TODO(), conn, cfg)
	defer pc.Close()

	allConnected.Done()

	payload := make([]byte, perfPayloadSizeMin+rand.Intn(perfPayloadSizeMax-perfPayloadSizeMin+1))
	packet := pktconn.NewPacket()
	packet.WriteBytes(payload)

	recvCh := pc.Recv()

	<-startSendRecv

	for i := 0; i < perfEchoCounterPerClient; i++ {
		pc.Send(packet)

		if _, ok := <-recvCh; !ok {
			panic(pc.Err())
		}

		atomic.AddInt32(&totalCounter, 1)
	}
}
