package pktconn

import (
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	MaxPayloadLength       = 32 * 1024 * 1024
	defaultRecvChanSize    = 100
	defaultFlushDelay      = time.Millisecond * 1
	defaultMaxFlushDelay   = time.Millisecond * 100
	defaultReadBufferSize  = 8192
	defaultWriteBufferSize = 8192

	payloadLengthSize = 4 // payloadLengthSize is the packet size field (uint32) size
	prePayloadSize    = payloadLengthSize
)

type Config struct {
	RecvChanSize    int           `json:"recv_chan_size"`
	FlushDelay      time.Duration `json:"flush_delay"`
	MaxFlushDelay   time.Duration `json:"max_flush_delay"`
	CrcChecksum     bool          `json:"crc_checksum"`
	ReadBufferSize  int           `json:"read_buffer_size"`
	WriteBufferSize int           `json:"write_buffer_size"`
	Tag             interface{}
}

func DefaultConfig() *Config {
	return &Config{
		RecvChanSize:    defaultRecvChanSize,
		FlushDelay:      defaultFlushDelay,
		MaxFlushDelay:   defaultMaxFlushDelay,
		CrcChecksum:     false,
		ReadBufferSize:  defaultReadBufferSize,
		WriteBufferSize: defaultWriteBufferSize,
		Tag:             nil,
	}
}

// PacketConn is a connection that send and receive data packets upon a network stream connection
type PacketConn struct {
	Tag    interface{}
	Config Config

	ctx                   context.Context
	conn                  net.Conn
	pendingPacketsLock    sync.Mutex
	pendingPackets        []*Packet
	waitPendingPacketsCnt int32
	gotPacketFlag         bool
	cancel                context.CancelFunc
	err                   error
	once                  uint32
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
		conn:   conn,
		Config: *cfg,
		ctx:    pcCtx,
		cancel: pcCancel,
		Tag:    cfg.Tag,
	}

	go pc.flushRoutine()
	return pc
}

func validateConfig(cfg *Config) {
	if cfg.FlushDelay < 0 {
		panic(fmt.Errorf("negative flush interval"))
	}

	if cfg.MaxFlushDelay < cfg.FlushDelay {
		panic(fmt.Errorf("please set max_flush_delay > flush_delay"))
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

func (pc *PacketConn) flushRoutine() {
	defer pc.Close()

	tickerInterval := pc.Config.FlushDelay
	if tickerInterval < time.Millisecond {
		tickerInterval = time.Millisecond
	}

	waitPendingPacketsCntLimit := int32(pc.Config.MaxFlushDelay / tickerInterval)
	if waitPendingPacketsCntLimit < 1 {
		waitPendingPacketsCntLimit = 1
	}

	ticker := time.NewTicker(tickerInterval)
	defer ticker.Stop()

	ctxDone := pc.ctx.Done()
loop:
	for {
		select {
		case <-ticker.C:
			err := pc.flush(waitPendingPacketsCntLimit)
			if err != nil {
				pc.closeWithError(err)
				break loop
			}
		case <-ctxDone:
			pc.closeWithError(pc.ctx.Err())
			break loop
		}
	}
}

func (pc *PacketConn) recvRoutine(recvChan chan *Packet, autoCloseChan bool) {
	if autoCloseChan {
		defer close(recvChan)
	}
	defer pc.Close()

	for {
		packet, err := pc.recv()
		if err != nil {
			pc.closeWithError(err)
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
	pc.gotPacketFlag = true
	pc.pendingPacketsLock.Unlock()
	return nil
}

// Flush connection writes
func (pc *PacketConn) flush(waitPendingPacketsCntLimit int32) (err error) {
	pc.pendingPacketsLock.Lock()
	gotPacketFlag := pc.gotPacketFlag
	pc.gotPacketFlag = false

	if len(pc.pendingPackets) == 0 { // no packets to send, common to happen, so handle efficiently
		pc.pendingPacketsLock.Unlock()
		return
	}

	// found pending packets to send
	pc.waitPendingPacketsCnt += 1
	if pc.waitPendingPacketsCnt < waitPendingPacketsCntLimit && gotPacketFlag {
		pc.pendingPacketsLock.Unlock()
		return
	}

	packets := make([]*Packet, 0, len(pc.pendingPackets))
	packets, pc.pendingPackets = pc.pendingPackets, packets
	pc.waitPendingPacketsCnt = 0
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
	err := writeFull(pc.conn, pdata)
	if err != nil {
		return err
	}

	if pc.Config.CrcChecksum {
		var crc32Buffer [4]byte
		payloadCrc := crc32.ChecksumIEEE(pdata)
		packetEndian.PutUint32(crc32Buffer[:], payloadCrc)
		return writeFull(pc.conn, crc32Buffer[:])
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
	err = readFull(pc.conn, uint32Buffer[:])
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
	err = readFull(pc.conn, payload)
	if err != nil {
		return nil, err
	}

	packet.SetPayloadLen(payloadSize)

	// receive checksum (uint32)
	if pc.Config.CrcChecksum {
		err = readFull(pc.conn, uint32Buffer[:])
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

func (pc *PacketConn) Recv(recvChan chan *Packet, autoCloseChan bool) <-chan *Packet {
	go pc.recvRoutine(recvChan, autoCloseChan)
	return recvChan
}

// Close the connection
func (pc *PacketConn) Close() error {
	return pc.closeWithError(io.EOF)
}

func (pc *PacketConn) closeWithError(err error) error {
	if atomic.CompareAndSwapUint32(&pc.once, 0, 1) {
		// close exactly once
		pc.err = err
		err := pc.conn.Close()
		pc.cancel()
		return err
	} else {
		return nil
	}
}

func (pc *PacketConn) Done() <-chan struct{} {
	return pc.ctx.Done()
}

func (pc *PacketConn) Err() error {
	return pc.err
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
