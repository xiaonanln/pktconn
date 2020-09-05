# pktconn
A high performance TCP Packet Connection library written in Go.
基于Go实现的高性能TCP数据封包通信库。

## Brief Introduction - 简介

1. A packet is a byte array of arbitrary length. 
    - 每个包都是一段任意长度的字节数组。
2. Provide `Packet` class for flexible data packing and unpacking.
    - 提供Packet类用于灵活的数据封包和拆包。
3. Make the APIs as simple as possible.
    - 让接口尽可能简单！

## Performance Benchmark - 性能测试
[tests/echo_server](https://github.com/xiaonanln/pktconn/blob/master/examples/server/server.go) can receive and send more than **54000** Packets of variable length (0 ~ 2048B).

[tests/echo_server](https://github.com/xiaonanln/pktconn/blob/master/examples/server/server.go)目前可以做到单核每秒接收并发回大于**5.4万**个0~2048字节的数据包。

* Testing Environment - 测试环境
    * [Github Action Virtual Machine](https://docs.github.com/en/actions/reference/virtual-environments-for-github-hosted-runners#supported-runners-and-hardware-resources)

The [profile result](https://raw.githubusercontent.com/xiaonanln/pktconn/master/tests/profile.pdf) shows that system 
calls contributed 85% of the runtime overhead, which is not optimizable. 
There is no memory allocation or GC overhead, which is the key to achieve high performance Packet sending and receiving. 

从[Profile结果](https://raw.githubusercontent.com/xiaonanln/pktconn/master/tests/profile.pdf)来看，**85%** 开销都在收发包的系统调用上，这部分是无法优化的。**几乎没有任何内存分配和垃圾回收的开销**，这也是实现高性能收发数据包的关键。

## Examples - 示例

### Server Example - 服务端示例

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

### Client Example - 客户端示例

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