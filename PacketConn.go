package packetconn

import (
	"fmt"
	"net"

	"sync"

	"time"

	"sync/atomic"
)

const (
	_MAX_PACKET_SIZE    = 25 * 1024 * 1024 // _MAX_PACKET_SIZE is the max size limit of packets in packet connections
	_SIZE_FIELD_SIZE    = 4                // _SIZE_FIELD_SIZE is the packet size field (uint32) size
	_PREPAYLOAD_SIZE    = _SIZE_FIELD_SIZE
	_MAX_PAYLOAD_LENGTH = _MAX_PACKET_SIZE - _PREPAYLOAD_SIZE
)

// PacketConn is a connection that send and receive data packets upon a network stream connection
type PacketConn struct {
	conn               net.Conn
	pendingPackets     []*Packet
	pendingPacketsLock sync.Mutex
	Recv               chan *Packet
}

// NewPacketConn creates a packet connection based on network connection
func NewPacketConn(conn net.Conn, recvChanSize int, flushInterval time.Duration) *PacketConn {
	pc := &PacketConn{
		conn: conn,
		Recv: make(chan *Packet, recvChanSize),
	}
	go pc.recvRoutine()
	go pc.flushRoutine(flushInterval)
	return pc
}

func (pc *PacketConn) flushRoutine(interval time.Duration) {
	for {
		err := pc.flush()
		if err != nil {
			break
		}

		time.Sleep(interval)
	}
}

func (pc *PacketConn) recvRoutine() {
	defer close(pc.Recv)
	defer pc.Close()

	for {
		packet, err := pc.recv()
		if err != nil {
			break
		}

		pc.Recv <- packet
	}
}

// Send send packets to remote
func (pc *PacketConn) Send(packet *Packet) error {
	if atomic.LoadInt64(&packet.refcount) <= 0 {
		panic(fmt.Errorf("sending packet with refcount=%d", packet.refcount))
	}

	packet.AddRefCount(1)
	pc.pendingPacketsLock.Lock()
	pc.pendingPackets = append(pc.pendingPackets, packet)
	pc.pendingPacketsLock.Unlock()
	return nil
}

// Flush connection writes
func (pc *PacketConn) flush() (err error) {
	pc.pendingPacketsLock.Lock()
	if len(pc.pendingPackets) == 0 { // no packets to send, common to happen, so handle efficiently
		pc.pendingPacketsLock.Unlock()
		return
	}
	packets := make([]*Packet, 0, len(pc.pendingPackets))
	packets, pc.pendingPackets = pc.pendingPackets, packets
	pc.pendingPacketsLock.Unlock()

	// flush should only be called in one goroutine

	if len(packets) == 1 {
		// only 1 packet to send, just send it directly, no need to use send buffer
		packet := packets[0]

		err = pc.writePacket(packet)
		packet.Release()
		if err == nil {
			err = tryFlush(pc.conn)
		}
		return
	}

	for _, packet := range packets {
		err = pc.writePacket(packet)
		packet.Release()

		if err != nil {
			break
		}
	}

	// now we send all data in the send buffer
	if err == nil {
		err = tryFlush(pc.conn)
	}
	return
}

func (pc *PacketConn) writePacket(packet *Packet) error {
	//var _crc32Buffer [4]byte
	//crc32Buffer := _crc32Buffer[:]

	pdata := packet.data()
	err := WriteAll(pc.conn, pdata)
	if err != nil {
		return err
	}

	return nil
	//payloadCrc := crc322.ChecksumIEEE(pdata)
	//packetEndian.PutUint32(crc32Buffer, payloadCrc)
	//return WriteAll(pc.conn, crc32Buffer)
}

// SetRecvDeadline sets the receive deadline
func (pc *PacketConn) SetRecvDeadline(deadline time.Time) error {
	return pc.conn.SetReadDeadline(deadline)
}

// recv receives the next packet
func (pc *PacketConn) recv() (*Packet, error) {
	var payloadSizeBuffer [4]byte
	//var crcChecksumBuffer [4]byte
	var err error

	err = ReadAll(pc.conn, payloadSizeBuffer[:])
	if err != nil {
		return nil, err
	}

	payloadSize := packetEndian.Uint32(payloadSizeBuffer[:])
	if payloadSize > _MAX_PAYLOAD_LENGTH {
		return nil, errPayloadTooLarge
	}

	// allocate a packet to receive payload
	packet := NewPacket()
	packet.Src = pc
	packet.AssureCapacity(payloadSize)
	err = ReadAll(pc.conn, packet.bytes[_PREPAYLOAD_SIZE:_PREPAYLOAD_SIZE+payloadSize])
	if err != nil {
		return nil, err
	}

	packet.setPayloadLen(payloadSize)
	return packet, nil
}

//func (pc *PacketConn) recvAll(b []byte) error {
//	return ReadAll(pc.conn, b)
//}

// Close the connection
func (pc *PacketConn) Close() error {
	return pc.conn.Close()
}

// RemoteAddr return the remote address
func (pc *PacketConn) RemoteAddr() net.Addr {
	return pc.conn.RemoteAddr()
}

// LocalAddr returns the local address
func (pc *PacketConn) LocalAddr() net.Addr {
	return pc.conn.LocalAddr()
}

func (pc *PacketConn) String() string {
	return fmt.Sprintf("[%s >>> %s]", pc.LocalAddr(), pc.RemoteAddr())
}
