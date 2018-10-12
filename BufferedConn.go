package packetconn

import (
	"bufio"
	"net"
)

// BufferedConn provides buffered write to connections
type BufferedConn struct {
	net.Conn
	bufReader *bufio.Reader
	bufWriter *bufio.Writer
}

// NewBufferedWriteConnection creates a new connection with buffered write based on underlying connection
func NewBufferedConn(conn net.Conn, readBufferSize, writeBufferSize int) *BufferedConn {
	brc := &BufferedConn{
		Conn: conn,
	}
	brc.bufReader = bufio.NewReaderSize(conn, readBufferSize)
	brc.bufWriter = bufio.NewWriterSize(conn, writeBufferSize)
	return brc
}

// Read
func (bc *BufferedConn) Read(p []byte) (int, error) {
	return bc.bufReader.Read(p)
}

func (bc *BufferedConn) Write(p []byte) (int, error) {
	return bc.bufWriter.Write(p)
}

func (bc *BufferedConn) Flush() error {
	err := bc.bufWriter.Flush()
	if err != nil {
		return err
	}

	if f, ok := bc.Conn.(flushable); ok {
		return f.Flush()
	}

	return nil
}
