# go-packetconn
基于Go实现的高性能TCP数据封包通信库。

## 简介
1. 每个包都是一段任意长度的字节数组
1. 提供Packet类用于灵活地封包和拆包
1. 让接口尽可能简单！

## 性能测试
[tests/echo_server](https://github.com/xiaonanln/go-packetconn/blob/master/examples/server/server.go)目前可以做到每秒接收并发回**55000**个0~2048字节的数据包
* 最多使用2个CPU：runtime.GOMAXPROCS(2)
* CPU信息：Intel(R) Xeon(R) CPU E5-2640 v2 @ 2.00GHz 32核
* 从profile结果来看，大部分开销都在收发包的系统调用上，这部分是无法优化的。**几乎没有任何内存分配和垃圾回收的开销**，这也是实现高性能收发数据包的关键。
```bash
Showing nodes accounting for 46.79s, 79.86% of 58.59s total
Dropped 151 nodes (cum <= 0.29s)
Showing top 50 nodes out of 92
      flat  flat%   sum%        cum   cum%
     0.34s  0.58%  0.58%     38.51s 65.73%  github.com/xiaonanln/go-packetconn.(*PacketConn).flushRoutine
     0.41s   0.7%  1.28%     35.30s 60.25%  github.com/xiaonanln/go-packetconn.(*PacketConn).flush
    32.31s 55.15% 56.43%     33.62s 57.38%  syscall.Syscall
     0.04s 0.068% 56.49%     31.12s 53.11%  github.com/xiaonanln/go-packetconn.tryFlush
     0.12s   0.2% 56.70%     30.95s 52.82%  github.com/xiaonanln/go-packetconn.(*bufferedConn).Flush
     0.13s  0.22% 56.92%     30.36s 51.82%  bufio.(*Writer).Flush
     0.09s  0.15% 57.07%     30.10s 51.37%  net.(*conn).Write
     0.31s  0.53% 57.60%        30s 51.20%  net.(*netFD).Write
     0.14s  0.24% 57.84%     29.69s 50.67%  internal/poll.(*FD).Write
     0.11s  0.19% 58.03%     28.22s 48.17%  syscall.Write
     0.23s  0.39% 58.42%     28.11s 47.98%  syscall.write
         0     0% 58.42%     11.34s 19.35%  github.com/xiaonanln/go-packetconn.(*PacketConn).recvRoutine
     0.46s  0.79% 59.21%     10.30s 17.58%  github.com/xiaonanln/go-packetconn.(*PacketConn).recv
     0.11s  0.19% 59.40%      8.64s 14.75%  github.com/xiaonanln/go-packetconn.ReadAll
     0.28s  0.48% 59.87%      8.53s 14.56%  github.com/xiaonanln/go-packetconn.(*bufferedConn).Read
     0.30s  0.51% 60.39%      8.25s 14.08%  bufio.(*Reader).Read
     0.16s  0.27% 60.66%      7.62s 13.01%  net.(*conn).Read
     0.43s  0.73% 61.39%      7.46s 12.73%  internal/poll.(*FD).Read
         0     0% 61.39%      7.46s 12.73%  net.(*netFD).Read
     0.04s 0.068% 61.46%      5.85s  9.98%  syscall.Read
     0.07s  0.12% 61.58%      5.81s  9.92%  syscall.read
     0.20s  0.34% 61.92%      3.84s  6.55%  runtime.mcall
     0.17s  0.29% 62.21%      3.21s  5.48%  runtime.park_m
     1.28s  2.18% 64.40%      2.84s  4.85%  runtime.selectgo
     0.21s  0.36% 64.76%      2.78s  4.74%  runtime.timerproc
     0.79s  1.35% 66.10%      2.60s  4.44%  runtime.schedule
     0.26s  0.44% 66.55%      2.40s  4.10%  runtime.chansend
```

## 示例

### 服务端示例
```go 
package main

import (
	"context"
	"fmt"
	"net"

	"github.com/xiaonanln/go-packetconn"
)

func main() {
	ln, err := net.Listen("tcp", "0.0.0.0:14572")

	if err != nil {
		panic(err)
	}

	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			panic(err)
		}

		go func() {
			pc := packetconn.NewPacketConn(context.TODO(), conn)
			fmt.Printf("client connected: %s\n", pc.RemoteAddr())

			for pkt := range pc.Recv() {
				fmt.Printf("recv packet: %d\n", pkt.GetPayloadLen())
				pc.Send(pkt) // send packet back to the client
				pkt.Release()
			}

			fmt.Printf("client disconnected: %s", pc.RemoteAddr())
		}()
	}
}

```

### 客户端示例

```go
package main

import (
	"context"
	"fmt"
	"net"

	"github.com/xiaonanln/go-packetconn"
)

func main() {
	conn, err := net.Dial("tcp", "localhost:14572")
	if err != nil {
		panic(err)
	}

	pc := packetconn.NewPacketConn(context.TODO(), conn)
	defer pc.Close()

	packet := packetconn.NewPacket()
	payload := make([]byte, 1024)
	packet.WriteBytes(payload)

	pc.Send(packet)
	recvPacket := <-pc.Recv()
	fmt.Printf("recv packet: %d\n", recvPacket.GetPayloadLen())
}
```