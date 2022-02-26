package xfs

import (
	"io"

	crand "crypto/rand"
)

func divide(x, y int64) int64 {
	return (x + y - 1) / y
}

func align(x, y int64) int64 {
	return divide(x, y) * y
}

type rngReader struct {
}

func (rng *rngReader) Read(b []byte) (n int, err error) {
	return crand.Read(b)
}

/*
	This function returns a newly writed UID for use in GPT data. It
	automatically handles generating the UID deterministically or
	non-deterministically as required.
*/
func generateUID() ([16]byte, error) {

	rng := new(rngReader)

	var uid [16]byte
	buf := make([]byte, 16)
	_, err := io.ReadFull(rng, buf)
	if err != nil {
		return uid, err
	}
	copy(uid[:], buf)
	uid[6] = uid[6]&^0xf0 | 0x40
	uid[8] = uid[8]&^0xc0 | 0x80
	return uid, nil
}
