package packetconn

import (
	"log"

	"github.com/pkg/errors"
)

var (
	errPayloadTooLarge = errors.Errorf("payload too large")
	errChecksumError   = errors.Errorf("checksum error")
)

type timeoutError interface {
	Timeout() bool // Is it a timeout error
}

type temperaryError interface {
	Temporary() bool
}

// IsTimeout checks if the error is a timeout error
func IsTimeout(err error) bool {
	log.Printf("IsTimeout: %v", err)
	if err == nil {
		return false
	}

	err = errors.Cause(err)
	ne, ok := err.(timeoutError)
	return ok && ne.Timeout()
}

// IsTimeout checks if the error is a timeout error
func IsTemporary(err error) bool {
	log.Printf("IsTemporary: %v", err)
	if err == nil {
		return false
	}

	err = errors.Cause(err)
	ne, ok := err.(temperaryError)
	return ok && ne.Temporary()
}

//// IsNetworkError check if the error is a connection error (close)
//func IsNetworkError(_err interface{}) bool {
//	err, ok := _err.(error)
//	if !ok {
//		return false
//	}
//
//	err = errors.Cause(err)
//	if err == io.EOF {
//		return true
//	}
//
//	neterr, ok := err.(net.Error)
//	if !ok {
//		return false
//	}
//	if neterr.Timeout() {
//		return false
//	}
//
//	return true
//}
