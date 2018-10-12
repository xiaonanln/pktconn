package packetconn

import (
	"github.com/pkg/errors"
	"io"
	"net"
)

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

// IsNetworkError check if the error is a connection error (close)
func IsNetworkError(_err interface{}) bool {
	err, ok := _err.(error)
	if !ok {
		return false
	}

	err = errors.Cause(err)
	if err == io.EOF {
		return true
	}

	neterr, ok := err.(net.Error)
	if !ok {
		return false
	}
	if neterr.Timeout() {
		return false
	}

	return true
}
