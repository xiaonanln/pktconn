package packetconn

import (
	"io"
	"net"
)

// WriteAll write all bytes of data to the writer
func WriteAll(conn io.Writer, data []byte) error {
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

		if err != nil && !IsTimeout(err) {
			return err
		}
	}
	return nil
}

// ReadAll reads from the reader until all bytes in data is filled
func ReadAll(conn io.Reader, data []byte) error {
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

		if err != nil && !IsTimeout(err) {
			return err
		}
	}
	return nil
}

// flushable is interface for flushable connections
type flushable interface {
	Flush() error
}

func tryFlush(conn net.Conn) error {
	if f, ok := conn.(flushable); ok {
		return f.Flush()
	} else {
		return nil
	}
}
