package pktconn

import (
	"io"
	"net"
	"runtime"
)

func writeFull(conn io.Writer, data []byte) error {
	left := len(data)
	for left > 0 {
		n, err := conn.Write(data)
		if n == left && err == nil { // handle most common case first
			return nil
		}

		if n > 0 {
			data = data[n:]
			left -= n
		}

		if err != nil {
			if !IsTemporary(err) {
				return err
			} else {
				runtime.Gosched()
			}
		}
	}
	return nil
}

func readFull(conn io.Reader, data []byte) error {
	left := len(data)
	for left > 0 {
		n, err := conn.Read(data)
		if n == left && err == nil { // handle most common case first
			return nil
		}

		if n > 0 {
			data = data[n:]
			left -= n
		}

		if err != nil {
			if !IsTemporary(err) {
				return err
			} else {
				runtime.Gosched()
			}
		}
	}
	return nil
}

type flushable interface {
	Flush() error
}

func tryFlush(conn net.Conn) error {
	if f, ok := conn.(flushable); ok {
		for {
			err := f.Flush()
			if err == nil || !IsTemporary(err) {
				return err
			} else {
				runtime.Gosched()
			}
		}
	} else {
		return nil
	}
}
