# go-packetconn
基于Go实现的包通信库。

## 简介
1. 每个包都是一段任意长度的字节数组
1. 提供Packet类用于灵活地封包和拆包
1. 让接口尽可能简单！
1. 性能测试：每秒接收并发回46万个1024字节的数据包
    * Intel(R) Xeon(R) CPU E5-2640 v2 @ 2.00GHz 32核

## 示例

### 服务端示例
```go 
package main

import (
	"fmt"
	"net"

	"github.com/xiaonanln/go-packetconn"
)

func main() {
	ln, err := net.Listen("tcp", "localhost:14572")

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
			pc := packetconn.NewPacketConn(conn)
			fmt.Printf("client connected: %s\n", pc.RemoteAddr())

			for pkt := range pc.Recv {
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
```