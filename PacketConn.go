package pktconn

import (
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"net"
	"sync/atomic"
	"time"

	"github.com/xiaonanln/tickchan"
)

const (
	MaxPayloadLength     = 32 * 1024 * 1024
	DefaultRecvChanSize  = 100
	defaultFlushDelay    = time.Millisecond * 1
	defaultMaxFlushDelay = time.Millisecond * 10
	sendChanSize         = 100

	payloadLengthSize = 4 // payloadLengthSize is the packet size field (uint32) size
	prePayloadSize    = payloadLengthSize
)

var (
	uniformTicker = tickchan.NewChanTicker(time.Millisecond)
)

type Config struct {
	FlushDelay    time.Duration `json:"flush_delay"`
	MaxFlushDelay time.Duration `json:"max_flush_delay"`
	CrcChecksum   bool          `json:"crc_checksum"`
	Tag           interface{}
}

func DefaultConfig() *Config {
	return &Config{
		FlushDelay:    defaultFlushDelay,
		MaxFlushDelay: defaultMaxFlushDelay,
		CrcChecksum:   false,
		Tag:           nil,
	}
}

// PacketConn is a connection that send and receive data packets upon a network stream connection
type PacketConn struct {
	Tag    interface{}
	Config Config

	ctx                   context.Context
	conn                  net.Conn
	sendChan              chan *Packet
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

	pcCtx, pcCancel := context.WithCancel(ctx)

	pc := &PacketConn{
		conn:     conn,
		Config:   *cfg,
		ctx:      pcCtx,
		cancel:   pcCancel,
		Tag:      cfg.Tag,
		sendChan: make(chan *Packet, sendChanSize),
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
}

func (pc *PacketConn) flushRoutine() {
	defer pc.Close()

	tickerChan := make(chan time.Time, 1)
	defer uniformTicker.Remove(tickerChan)

	ctxDone := pc.ctx.Done()
	var firstPacketArriveTime, lastPacketArriveTime time.Time
	var packets []*Packet
loop:
	for {
		select {
		case packet := <-pc.sendChan:
			now := uniformTicker.LastTickTime()
			packets = append(packets, packet)
			lastPacketArriveTime = now
			if len(packets) == 1 {
				firstPacketArriveTime = now
				uniformTicker.Add(tickerChan)
			}
		case now := <-tickerChan:
			if len(packets) > 0 {
				if now.Sub(lastPacketArriveTime) >= pc.Config.FlushDelay+time.Millisecond || now.Sub(firstPacketArriveTime) >= pc.Config.MaxFlushDelay {
					// time to send the packet
					uniformTicker.Remove(tickerChan)
					err := pc.flush(packets)
					packets = nil
					if err != nil {
						pc.closeWithError(err)
						break loop
					}
				}
			}
		case <-ctxDone:
			pc.closeWithError(pc.ctx.Err())
			break loop
		}
	}
}

func (pc *PacketConn) Recv() <-chan *Packet {
	return pc.RecvChanSize(DefaultRecvChanSize)
}

func (pc *PacketConn) RecvChanSize(chanSize uint) <-chan *Packet {
	recvChan := make(chan *Packet, chanSize)

	go func() {
		defer close(recvChan)
		_ = pc.RecvChan(recvChan)
	}()

	return recvChan
}

func (pc *PacketConn) RecvChan(recvChan chan *Packet) (err error) {
	for {
		packet, err := pc.recv()
		if err != nil {
			_ = pc.closeWithError(err)
			break
		}

		recvChan <- packet
	}

	return
}

// Send send packets to remote
func (pc *PacketConn) Send(packet *Packet) {
	if atomic.LoadInt64(&packet.refcount) <= 0 {
		panic(fmt.Errorf("sending packet with refcount=%d", packet.refcount))
	}

	// using select to avoid stucking when the channel is full
	select {
	case pc.sendChan <- packet:
		packet.addRefCount(1)
	default:
		break
	}

}

// Flush connection writes
func (pc *PacketConn) flush(packets []*Packet) (err error) {
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
