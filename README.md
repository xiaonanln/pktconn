# pktconn
基于Go实现的高性能TCP数据封包通信库。

## 简介


1. 每个包都是一段任意长度的字节数组
1. 提供Packet类用于灵活地封包和拆包
1. 让接口尽可能简单！

## 性能测试
[tests/echo_server](https://github.com/xiaonanln/pktconn/blob/master/examples/server/server.go)目前可以做到单核每秒接收并发回**3.5万**个0~2048字节的数据包
* 测试环境
    * 操作系统：Debian 8
    * CPU：Intel(R) Xeon(R) CPU E5-2640 v2 @ 2.00GHz

从[Profile结果](https://raw.githubusercontent.com/xiaonanln/pktconn/master/tests/profile.pdf)来看，**85%** 开销都在收发包的系统调用上，这部分是无法优化的。**几乎没有任何内存分配和垃圾回收的开销**，这也是实现高性能收发数据包的关键。

## 示例

### 服务端示例

```go 
package main

import (
	"context"
	"fmt"
	"net"

	"github.com/xiaonanln/pktconn"
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
			pc := pktconn.NewPacketConn(context.TODO(), conn)
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
```