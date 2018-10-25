package tests

import (
	"testing"

	"github.com/xiaonanln/pktconn"
)

func TestNewPacket(t *testing.T) {
	pkt := pktconn.NewPacket()
	if pkt.GetReadPos() != 0 {
		t.Fatalf("read pos != 0")
	}
	if pkt.GetPayloadLen() != 0 {
		t.Fatalf("payload len != 0")
	}
	pl := pkt.Payload()
	if len(pl) != 0 {
		t.Fatalf("payload is not empty")
	}
	if len(pkt.UnreadPayload()) != 0 {
		t.Fatalf("unread payload is not empty")
	}
	if pkt.HasUnreadPayload() {
		t.Fatalf("HasUnreadPayload error")
	}
}

func TestPacketRelease(t *testing.T) {
	pkt := pktconn.NewPacket()
	pkt.WriteBytes(make([]byte, 10))
	pkt.WriteBytes(make([]byte, 100))
	pkt.WriteBytes(make([]byte, 1000))
	pkt.WriteBytes(make([]byte, 10000))
	pkt.WriteBytes(make([]byte, 100000))
	pkt.Release()
}

func TestPacketGeneral(t *testing.T) {
	pkt := pktconn.NewPacket()
	pkt.WriteUint16(0xABCD)
	if !pkt.HasUnreadPayload() {
		t.Fatalf("HasUnreadPayload error")
	}
	if pkt.ReadUint16() != 0xABCD {
		t.Fatalf("read wrong")
	}

	pkt.WriteBool(true)
	if pkt.ReadBool() != true {
		t.Fatalf("read wrong")
	}
	pkt.WriteBool(false)
	if pkt.ReadBool() != false {
		t.Fatalf("read wrong")
	}

	pkt.WriteUint32(0xABCD1234)
	if pkt.ReadUint32() != 0xABCD1234 {
		t.Fatalf("read wrong")
	}
	pkt.WriteUint64(0xABCD123456780000)
	if pkt.ReadUint64() != 0xABCD123456780000 {
		t.Fatalf("read wrong")
	}
	pkt.WriteOneByte(0xAA)
	if pkt.ReadOneByte() != 0xAA {
		t.Fatalf("read wrong")
	}
	pkt.WriteBytes([]byte("hello"))
	if string(pkt.ReadBytes(5)) != "hello" {
		t.Fatalf("read wrong")
	}
	pkt.WriteVarBytesH([]byte("hello2"))
	if string(pkt.ReadVarBytesH()) != "hello2" {
		t.Fatalf("read wrong")
	}
	pkt.WriteVarBytesI([]byte("hello3"))
	if string(pkt.ReadVarBytesI()) != "hello3" {
		t.Fatalf("read wrong")
	}

	pkt.Release()
}

func TestPacketWriteMaxPayloadLen(t *testing.T) {
	pkt := pktconn.NewPacket()
	pkt.WriteBytes(make([]byte, pktconn.MaxPayloadLength))
}

func TestPacketWritePayloadTooLarge1(t *testing.T) {
	defer func() {
		err := recover()
		if err != pktconn.ErrPayloadTooLarge {
			panic("wrong err")
		}
	}()
	pkt := pktconn.NewPacket()
	pkt.WriteBytes(make([]byte, pktconn.MaxPayloadLength+1))
}

func TestPacketWritePayloadTooLarge2(t *testing.T) {
	defer func() {
		err := recover()
		if err != pktconn.ErrPayloadTooLarge {
			panic("wrong err")
		}
	}()
	pkt := pktconn.NewPacket()
	pkt.WriteBytes(make([]byte, pktconn.MaxPayloadLength/2))
	pkt.WriteBytes(make([]byte, pktconn.MaxPayloadLength/2))
	pkt.WriteBytes(make([]byte, pktconn.MaxPayloadLength/2))
}
