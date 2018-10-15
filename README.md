# go-packetconn
基于Go实现的包通信库。

1. 每个包都是一段任意长度的字节数组
2. 提供Packet类用于灵活地封包和拆包
3. 性能测试：每秒收发46万个1024字节的包
    * Intel(R) Xeon(R) CPU E5-2640 v2 @ 2.00GHz 32核
