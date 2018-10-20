package packetconn

import (
	"context"
	"fmt"
	"hash/crc32"
	"net"

	"sync"

	"time"

	"sync/atomic"
)

const (
	MaxPayloadLength       = 32 * 1024 * 1024
	defaultRecvChanSize    = 1000
	defaultFlushInterval   = time.Millisecond * 5
	defaultReadBufferSize  = 16384
	defaultWriteBufferSize = 16384

	payloadLengthSize = 4 // payloadLengthSize is the packet size field (uint32) size
	prePayloadSize    = payloadLengthSize
)

type Config struct {
	RecvChanSize    int           `json:"recv_chan_size"`
	FlushInterval   time.Duration `json:"flush_interval"`
	CrcChecksum     bool          `json:"crc_checksum"`
	ReadBufferSize  int           `json:"read_buffer_size"`
	WriteBufferSize int           `json:"write_buffer_size"`
}

func DefaultConfig() *Config {
	return &Config{
		RecvChanSize:    defaultRecvChanSize,
		FlushInterval:   defaultFlushInterval,
		CrcChecksum:     true,
		ReadBufferSize:  defaultReadBufferSize,
		WriteBufferSize: defaultWriteBufferSize,
	}
}

// PacketConn is a connection that send and receive data packets upon a network stream connection
type PacketConn struct {
	conn               net.Conn
	pendingPackets     []*Packet
	pendingPacketsLock sync.Mutex
	recvChan           chan *Packet
	Config             Config
	ctx                context.Context
	cancel             context.CancelFunc
}

// NewPacketConn creates a packet connection based on network connection
func NewPacketConn(ctx context.Context, conn net.Conn) *PacketConn {
	return NewPacketConnWithConfig(ctx, conn, DefaultConfig())
}

func NewPacketConnWithConfig(ctx context.Context, conn net.Conn, cfg *Config) *PacketConn {
	if conn == nil {
		panic(fmt.Errorf("conn is nil"))
	}

	if cfg == nil {
		cfg = DefaultConfig()
	}

	validateConfig(cfg)

	if cfg.ReadBufferSize > 0 || cfg.WriteBufferSize > 0 {
		conn = newBufferedConn(conn, cfg.ReadBufferSize, cfg.WriteBufferSize)
	}

	pcCtx, pcCancel := context.WithCancel(ctx)
	pc := &PacketConn{
		conn:     conn,
		recvChan: make(chan *Packet, cfg.RecvChanSize),
		Config:   *cfg,
		ctx:      pcCtx,
		cancel:   pcCancel,
	}

	go pc.recvRoutine()
	go pc.flushRoutine(cfg.FlushInterval)
	return pc
}

func validateConfig(cfg *Config) {
	if cfg.FlushInterval < 0 {
		panic(fmt.Errorf("negative flush interval"))
	}

	if cfg.RecvChanSize < 0 {
		panic(fmt.Errorf("negative recv chan size"))
	}

	if cfg.ReadBufferSize < 0 {
		panic(fmt.Errorf("negative read buffer size"))
	}

	if cfg.WriteBufferSize < 0 {
		panic(fmt.Errorf("negative write buffer size"))
	}
}

func (pc *PacketConn) flushRoutine(interval time.Duration) {
	defer pc.Close()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	ctxDone := pc.ctx.Done()
loop:
	for {
		select {
		case <-ticker.C:
			err := pc.flush()
			if err != nil {
				break loop
			}
		case <-ctxDone:
			break loop
		}
	}
}

func (pc *PacketConn) recvRoutine() {
	defer pc.Close()
	recvChan := pc.recvChan
	defer close(recvChan)

	for {
		packet, err := pc.recv()
		if err != nil {
			break
		}

		recvChan <- packet
	}
}

// Send send packets to remote
func (pc *PacketConn) Send(packet *Packet) error {
	if atomic.LoadInt64(&packet.refcount) <= 0 {
		panic(fmt.Errorf("sending packet with refcount=%d", packet.refcount))
	}

	packet.addRefCount(1)
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
	pdata := packet.data()
	err := WriteAll(pc.conn, pdata)
	if err != nil {
		return err
	}

	if pc.Config.CrcChecksum {
		var crc32Buffer [4]byte
		payloadCrc := crc32.ChecksumIEEE(pdata)
		packetEndian.PutUint32(crc32Buffer[:], payloadCrc)
		return WriteAll(pc.conn, crc32Buffer[:])
	} else {
		return nil
	}
}

// recv receives the next packet
func (pc *PacketConn) recv() (*Packet, error) {
	var uint32Buffer [4]byte
	//var crcChecksumBuffer [4]byte
	var err error

	// receive payload length (uint32)
	err = ReadAll(pc.conn, uint32Buffer[:])
	if err != nil {
		return nil, err
	}

	payloadSize := packetEndian.Uint32(uint32Buffer[:])
	if payloadSize > MaxPayloadLength {
		return nil, errPayloadTooLarge
	}

	// allocate a packet to receive payload
	packet := NewPacket()
	packet.Src = pc
	payload := packet.extendPayload(int(payloadSize))
	err = ReadAll(pc.conn, payload)
	if err != nil {
		return nil, err
	}

	packet.SetPayloadLen(payloadSize)

	// receive checksum (uint32)
	if pc.Config.CrcChecksum {
		err = ReadAll(pc.conn, uint32Buffer[:])
		if err != nil {
			return nil, err
		}

		payloadCrc := crc32.ChecksumIEEE(packet.data())
		if payloadCrc != packetEndian.Uint32(uint32Buffer[:]) {
			return nil, errChecksumError
		}
	}

	return packet, nil
}

func (pc *PacketConn) Recv() <-chan *Packet {
	return pc.recvChan
}

// Close the connection
func (pc *PacketConn) Close() error {
	pc.cancel()
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
	return fmt.Sprintf("PacketConn<%s-%s>", pc.LocalAddr(), pc.RemoteAddr())
}
