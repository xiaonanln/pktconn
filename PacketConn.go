package packetconn

import (
	"fmt"
	"net"

	"encoding/binary"

	"sync"

	"time"

	"sync/atomic"

	"github.com/pkg/errors"
	simplelogger "github.com/xiaonanln/go-simplelogger"
)

const (
	_MAX_PACKET_SIZE    = 25 * 1024 * 1024 // _MAX_PACKET_SIZE is the max size limit of packets in packet connections
	_SIZE_FIELD_SIZE    = 4                // _SIZE_FIELD_SIZE is the packet size field (uint32) size
	_PREPAYLOAD_SIZE    = _SIZE_FIELD_SIZE
	_MAX_PAYLOAD_LENGTH = _MAX_PACKET_SIZE - _PREPAYLOAD_SIZE
)

var (
	// NETWORK_ENDIAN is the network Endian of connections
	NETWORK_ENDIAN = binary.LittleEndian
	errRecvAgain   = _ErrRecvAgain{}
)

type _ErrRecvAgain struct{}

func (err _ErrRecvAgain) Error() string {
	return "recv again"
}

func (err _ErrRecvAgain) Temporary() bool {
	return true
}

func (err _ErrRecvAgain) Timeout() bool {
	return true
}

// PacketConn is a connection that send and receive data packets upon a network stream connection
type PacketConn struct {
	conn               net.Conn
	pendingPackets     []*Packet
	pendingPacketsLock sync.Mutex

	// buffers and infos for receiving a packet
	payloadLenBuf         [_SIZE_FIELD_SIZE]byte
	payloadLenBytesRecved int
	recvTotalPayloadLen   uint32
	recvedPayloadLen      uint32
	recvingPacket         *Packet
}

// NewPacketConn creates a packet connection based on network connection
func NewPacketConn(conn net.Conn) *PacketConn {
	pc := &PacketConn{
		conn: conn,
	}
	return pc
}

// NewPacket allocates a new packet (usually for sending)
func (pc *PacketConn) NewPacket() *Packet {
	return allocPacket()
}

// SendPacket send packets to remote
func (pc *PacketConn) SendPacket(packet *Packet) error {
	if atomic.LoadInt64(&packet.refcount) <= 0 {
		simplelogger.Panicf("sending packet with refcount=%d", packet.refcount)
	}

	packet.AddRefCount(1)
	pc.pendingPacketsLock.Lock()
	pc.pendingPackets = append(pc.pendingPackets, packet)
	pc.pendingPacketsLock.Unlock()
	return nil
}

// Flush connection writes
func (pc *PacketConn) Flush() (err error) {
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

		err = WriteAll(pc.conn, packet.data())
		packet.Release()
		if err == nil {
			err = tryFlush(pc.conn)
		}
		return
	}

	for _, packet := range packets {
		WriteAll(pc.conn, packet.data())
		packet.Release()
	}

	// now we send all data in the send buffer
	if err == nil {
		err = tryFlush(pc.conn)
	}
	return
}

// SetRecvDeadline sets the receive deadline
func (pc *PacketConn) SetRecvDeadline(deadline time.Time) error {
	return pc.conn.SetReadDeadline(deadline)
}

// RecvPacket receives the next packet
func (pc *PacketConn) RecvPacket() (*Packet, error) {
	if pc.payloadLenBytesRecved < _SIZE_FIELD_SIZE {
		// receive more of payload len bytes
		n, err := pc.conn.Read(pc.payloadLenBuf[pc.payloadLenBytesRecved:])
		pc.payloadLenBytesRecved += n
		if pc.payloadLenBytesRecved < _SIZE_FIELD_SIZE {
			if err == nil {
				err = errRecvAgain
			}
			return nil, err // packet not finished yet
		}

		pc.recvTotalPayloadLen = NETWORK_ENDIAN.Uint32(pc.payloadLenBuf[:])

		if pc.recvTotalPayloadLen == 0 || pc.recvTotalPayloadLen > _MAX_PAYLOAD_LENGTH {
			err := errors.Errorf("invalid payload length: %v", pc.recvTotalPayloadLen)
			pc.resetRecvStates()
			pc.Close()
			return nil, err
		}

		pc.recvedPayloadLen = 0
		pc.recvingPacket = NewPacket()
		pc.recvingPacket.AssureCapacity(pc.recvTotalPayloadLen)
	}

	// now all bytes of payload len is received, start receiving payload
	n, err := pc.conn.Read(pc.recvingPacket.bytes[_PREPAYLOAD_SIZE+pc.recvedPayloadLen : _PREPAYLOAD_SIZE+pc.recvTotalPayloadLen])
	pc.recvedPayloadLen += uint32(n)

	if pc.recvedPayloadLen == pc.recvTotalPayloadLen {
		// full packet received, return the packet
		packet := pc.recvingPacket
		packet.setPayloadLen(pc.recvTotalPayloadLen)
		pc.resetRecvStates()

		return packet, nil
	}

	if err == nil {
		err = errRecvAgain
	}
	return nil, err
}

func (pc *PacketConn) resetRecvStates() {
	pc.payloadLenBytesRecved = 0
	pc.recvTotalPayloadLen = 0
	pc.recvedPayloadLen = 0
	pc.recvingPacket = nil
}

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
