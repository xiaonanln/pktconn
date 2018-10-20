package packetconn

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sync"

	"unsafe"

	"sync/atomic"
)

const (
	minPayloadCap       = 128
	payloadCapGrowShift = uint(2)
)

var (
	packetEndian               = binary.LittleEndian
	predefinePayloadCapacities []uint32

	packetBufferPools = map[uint32]*sync.Pool{}
	packetPool        = &sync.Pool{
		New: func() interface{} {
			p := &Packet{}
			p.bytes = p.initialBytes[:]
			return p
		},
	}
)

func init() {
	payloadCap := uint32(minPayloadCap) << payloadCapGrowShift
	for payloadCap < MaxPayloadLength {
		predefinePayloadCapacities = append(predefinePayloadCapacities, payloadCap)
		payloadCap <<= payloadCapGrowShift
	}
	predefinePayloadCapacities = append(predefinePayloadCapacities, MaxPayloadLength)

	for _, payloadCap := range predefinePayloadCapacities {
		payloadCap := payloadCap
		packetBufferPools[payloadCap] = &sync.Pool{
			New: func() interface{} {
				return make([]byte, prePayloadSize+payloadCap)
			},
		}
	}
}

func getPayloadCapOfPayloadLen(payloadLen uint32) uint32 {
	for _, payloadCap := range predefinePayloadCapacities {
		if payloadCap >= payloadLen {
			return payloadCap
		}
	}
	return MaxPayloadLength
}

// Packet is a packet for sending data
type Packet struct {
	Src          *PacketConn
	readCursor   uint32
	refcount     int64
	bytes        []byte
	initialBytes [prePayloadSize + minPayloadCap]byte
}

func allocPacket() *Packet {
	pkt := packetPool.Get().(*Packet)
	pkt.refcount = 1

	if pkt.PayloadLen() != 0 {
		panic(fmt.Errorf("allocPacket: payload should be 0, but is %d", pkt.PayloadLen()))
	}

	return pkt
}

// NewPacket allocates a new packet
func NewPacket() *Packet {
	return allocPacket()
}

func (p *Packet) payloadSlice(i, j uint32) []byte {
	return p.bytes[i+prePayloadSize : j+prePayloadSize]
}

// PayloadLen returns the payload length
func (p *Packet) PayloadLen() uint32 {
	packetEndian.Uint32(p.bytes[0:4])
	return *(*uint32)(unsafe.Pointer(&p.bytes[0]))
}

func (p *Packet) SetPayloadLen(plen uint32) {
	pplen := (*uint32)(unsafe.Pointer(&p.bytes[0]))
	*pplen = plen
}

// Payload returns the total payload of packet
func (p *Packet) Payload() []byte {
	return p.bytes[prePayloadSize : prePayloadSize+p.PayloadLen()]
}

// UnreadPayload returns the unread payload
func (p *Packet) UnreadPayload() []byte {
	pos := p.readCursor + prePayloadSize
	payloadEnd := prePayloadSize + p.PayloadLen()
	return p.bytes[pos:payloadEnd]
}

// HasUnreadPayload returns if all payload is read
func (p *Packet) HasUnreadPayload() bool {
	pos := p.readCursor + prePayloadSize
	plen := p.PayloadLen()
	return pos < plen
}

func (p *Packet) data() []byte {
	return p.bytes[0 : prePayloadSize+p.PayloadLen()]
}

// PayloadCap returns the current payload capacity
func (p *Packet) PayloadCap() uint32 {
	return uint32(len(p.bytes) - prePayloadSize)
}

func (p *Packet) extendPayload(size int) []byte {
	if size > MaxPayloadLength {
		return nil
	}

	payloadLen := p.PayloadLen()
	newPayloadLen := payloadLen + uint32(size)
	oldCap := p.PayloadCap()

	if newPayloadLen <= oldCap { // most case
		p.SetPayloadLen(newPayloadLen)
		return p.payloadSlice(payloadLen, newPayloadLen)
	}

	if newPayloadLen > MaxPayloadLength {
		return nil
	}

	// try to find the proper capacity for the size bytes
	resizeToCap := getPayloadCapOfPayloadLen(newPayloadLen)

	buffer := packetBufferPools[resizeToCap].Get().([]byte)
	if len(buffer) != int(resizeToCap+prePayloadSize) {
		panic(fmt.Errorf("buffer size should be %d, but is %d", resizeToCap, len(buffer)))
	}
	copy(buffer, p.data())
	oldBytes := p.bytes
	p.bytes = buffer

	if oldCap > minPayloadCap {
		// release old bytes
		packetBufferPools[oldCap].Put(oldBytes)
	}

	p.SetPayloadLen(newPayloadLen)
	return p.payloadSlice(payloadLen, newPayloadLen)
}

// AddRefCount adds reference count of packet
func (p *Packet) AddRefCount(add int64) {
	atomic.AddInt64(&p.refcount, add)
}

// Release releases the packet to packet pool
func (p *Packet) Release() {
	refcount := atomic.AddInt64(&p.refcount, -1)

	if refcount == 0 {
		p.Src = nil

		payloadCap := p.PayloadCap()
		if payloadCap > minPayloadCap {
			buffer := p.bytes
			p.bytes = p.initialBytes[:]
			packetBufferPools[payloadCap].Put(buffer) // reclaim the buffer
		}

		p.readCursor = 0
		p.SetPayloadLen(0)
		packetPool.Put(p)
	} else if refcount < 0 {
		panic(fmt.Errorf("releasing packet with refcount=%d", p.refcount))
	}
}

// ClearPayload clears packet payload
func (p *Packet) ClearPayload() {
	p.readCursor = 0
	p.SetPayloadLen(0)
}

func (p *Packet) SetReadPos(pos uint32) {
	plen := p.PayloadLen()
	if pos > plen {
		pos = plen
	}

	p.readCursor = pos
}

func (p *Packet) ReadPos() uint32 {
	return p.readCursor
}

// WriteOneByte appends one byte to the end of payload
func (p *Packet) WriteOneByte(b byte) {
	pl := p.extendPayload(1)
	pl[0] = b
}

// ReadOneByte reads one byte from the beginning
func (p *Packet) ReadOneByte() (v byte) {
	pos := p.readCursor + prePayloadSize
	v = p.bytes[pos]
	p.readCursor += 1
	return
}

// WriteBool appends one byte 1/0 to the end of payload
func (p *Packet) WriteBool(b bool) {
	if b {
		p.WriteOneByte(1)
	} else {
		p.WriteOneByte(0)
	}
}

// ReadBool reads one byte 1/0 from the beginning of unread payload
func (p *Packet) ReadBool() (v bool) {
	return p.ReadOneByte() != 0
}

// WriteUint16 appends one uint16 to the end of payload
func (p *Packet) WriteUint16(v uint16) {
	pl := p.extendPayload(2)
	packetEndian.PutUint16(pl, v)
}

// WriteUint32 appends one uint32 to the end of payload
func (p *Packet) WriteUint32(v uint32) {
	pl := p.extendPayload(4)
	packetEndian.PutUint32(pl, v)
}

// PopUint32 pops one uint32 from the end of payload
func (p *Packet) PopUint32() (v uint32) {
	payloadEnd := prePayloadSize + p.PayloadLen()
	v = packetEndian.Uint32(p.bytes[payloadEnd-4 : payloadEnd])
	*(*uint32)(unsafe.Pointer(&p.bytes[0])) -= 4
	return
}

// WriteUint64 appends one uint64 to the end of payload
func (p *Packet) WriteUint64(v uint64) {
	pl := p.extendPayload(8)
	packetEndian.PutUint64(pl, v)
	*(*uint32)(unsafe.Pointer(&p.bytes[0])) += 8
}

// WriteFloat32 appends one float32 to the end of payload
func (p *Packet) WriteFloat32(f float32) {
	p.WriteUint32(math.Float32bits(f))
}

// ReadFloat32 reads one float32 from the beginning of unread payload
func (p *Packet) ReadFloat32() float32 {
	return math.Float32frombits(p.ReadUint32())
}

// WriteFloat64 appends one float64 to the end of payload
func (p *Packet) WriteFloat64(f float64) {
	p.WriteUint64(math.Float64bits(f))
}

// ReadFloat64 reads one float64 from the beginning of unread payload
func (p *Packet) ReadFloat64() float64 {
	return math.Float64frombits(p.ReadUint64())
}

// Write write bytes to the packet payload. If the payload size reaches limit, Write returns 0, nil
// Implements io.Writer interface
func (p *Packet) Write(b []byte) (n int, err error) {
	defer func() {
		err := recover()
		if err != nil {
			n, err = 0, io.EOF
		}
	}()
	p.WriteBytes(b)
	return len(b), nil
}

// WriteBytes appends slice of bytes to the end of payload
func (p *Packet) WriteBytes(b []byte) int {
	bl := len(b)
	pl := p.extendPayload(bl)
	if pl != nil {
		copy(pl, b)
		return bl
	} else {
		return 0
	}
}

// WriteVarStr appends a varsize string to the end of payload
func (p *Packet) WriteVarStr(s string) int {
	return p.WriteVarBytesH([]byte(s))
}

// WriteVarBytesI appends varsize bytes to the end of payload
func (p *Packet) WriteVarBytesI(b []byte) int {
	bl := len(b)
	if bl > MaxPayloadLength {
		return 0
	}

	pl := p.extendPayload(bl + 4)
	packetEndian.PutUint32(pl[:4], uint32(bl))
	copy(pl[4:], b)
	return bl + 4
}

// WriteVarBytesH appends varsize bytes to the end of payload
func (p *Packet) WriteVarBytesH(b []byte) int {
	bl := len(b)
	if bl > MaxPayloadLength {
		return 0
	}

	pl := p.extendPayload(bl + 2)
	packetEndian.PutUint16(pl[:2], uint16(bl))
	copy(pl[2:], b)
	return bl + 2
}

// ReadUint16 reads one uint16 from the beginning of unread payload
func (p *Packet) ReadUint16() (v uint16) {
	pos := p.readCursor + prePayloadSize
	v = packetEndian.Uint16(p.bytes[pos : pos+2])
	p.readCursor += 2
	return
}

// ReadInt16 reads one int16 from the beginning of unread payload
func (p *Packet) ReadInt16() (v int16) {
	pos := p.readCursor + prePayloadSize
	*(*uint16)(unsafe.Pointer(&v)) = packetEndian.Uint16(p.bytes[pos : pos+2])
	p.readCursor += 2
	return
}

// ReadUint32 reads one uint32 from the beginning of unread payload
func (p *Packet) ReadUint32() (v uint32) {
	pos := p.readCursor + prePayloadSize
	v = packetEndian.Uint32(p.bytes[pos : pos+4])
	p.readCursor += 4
	return
}

// ReadUint64 reads one uint64 from the beginning of unread payload
func (p *Packet) ReadUint64() (v uint64) {
	pos := p.readCursor + prePayloadSize
	v = packetEndian.Uint64(p.bytes[pos : pos+8])
	p.readCursor += 8
	return
}

// ReadBytes reads bytes from the beginning of unread payload
func (p *Packet) ReadBytes(size uint32) []byte {
	pos := p.readCursor + prePayloadSize
	if pos > uint32(len(p.bytes)) || pos+size > uint32(len(p.bytes)) {
		panic(fmt.Errorf("Packet %p bytes is %d, but reading %d+%d", p, len(p.bytes), pos, size))
	}

	bytes := p.bytes[pos : pos+size] // bytes are not copied
	p.readCursor += size
	return bytes
}

// ReadVarStr reads a varsize string from the beginning of unread  payload
func (p *Packet) ReadVarStr() string {
	b := p.ReadVarBytesH()
	return string(b)
}

// ReadVarBytesI reads a varsize slice of bytes from the beginning of unread payload
func (p *Packet) ReadVarBytesI() []byte {
	blen := p.ReadUint32()
	return p.ReadBytes(blen)
}

// ReadVarBytesH reads a varsize slice of bytes from the beginning of unread payload
func (p *Packet) ReadVarBytesH() []byte {
	blen := p.ReadUint16()
	return p.ReadBytes(uint32(blen))
}

func (p *Packet) WriteMapStringString(m map[string]string) {
	p.WriteUint32(uint32(len(m)))
	for k, v := range m {
		p.WriteVarStr(k)
		p.WriteVarStr(v)
	}
}

func (p *Packet) ReadMapStringString() map[string]string {
	size := p.ReadUint32()
	m := make(map[string]string, size)
	for i := uint32(0); i < size; i++ {
		k := p.ReadVarStr()
		v := p.ReadVarStr()
		m[k] = v
	}
	return m
}

// WriteStringList appends a list of strings to the end of payload
func (p *Packet) WriteStringList(list []string) {
	p.WriteUint16(uint16(len(list)))
	for _, s := range list {
		p.WriteVarStr(s)
	}
}

// ReadStringList reads a list of strings from the beginning of unread payload
func (p *Packet) ReadStringList() []string {
	listlen := int(p.ReadUint16())
	list := make([]string, listlen)
	for i := 0; i < listlen; i++ {
		list[i] = p.ReadVarStr()
	}
	return list
}
