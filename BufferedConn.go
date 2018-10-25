package pktconn

import (
	"bufio"
	"net"
)

// bufferedConn provides buffered write to connections
type bufferedConn struct {
	net.Conn
	bufReader *bufio.Reader
	bufWriter *bufio.Writer
}

// NewBufferedConn creates a new connection with buffered write based on underlying connection
func newBufferedConn(conn net.Conn, readBufferSize, writeBufferSize int) *bufferedConn {
	brc := &bufferedConn{
		Conn: conn,
	}
	brc.bufReader = bufio.NewReaderSize(conn, readBufferSize)
	brc.bufWriter = bufio.NewWriterSize(conn, writeBufferSize)
	return brc
}

// Read
func (bc *bufferedConn) Read(p []byte) (int, error) {
	return bc.bufReader.Read(p)
}

func (bc *bufferedConn) Write(p []byte) (int, error) {
	return bc.bufWriter.Write(p)
}

func (bc *bufferedConn) Flush() error {
	err := bc.bufWriter.Flush()
	if err != nil {
		return err
	}

	if f, ok := bc.Conn.(flushable); ok {
		return f.Flush()
	}

	return nil
}
