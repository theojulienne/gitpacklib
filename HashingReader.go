package gitpacklib

import (
	"crypto/sha1"
	"hash"
	"io"
)

// HashingReader proxies an io.Reader with optional io.ByteReader support,
// and hashes all bytes that pass through with a given hash.Hash.
//
// Reset and Sum methods are also proxied to the internal Hash object.
type HashingReader struct {
	h hash.Hash
	r io.Reader
}

func NewHashingReader(h hash.Hash, r io.Reader) *HashingReader {
	return &HashingReader{h, r}
}

func NewSHA1Reader(r io.Reader) *HashingReader {
	return &HashingReader{sha1.New(), r}
}

func (hr *HashingReader) Read(p []byte) (n int, err error) {
	n, err = hr.r.Read(p)
	if err == nil {
		hr.h.Write(p[:n])
	}
	return n, err
}

func (hr *HashingReader) ReadByte() (c byte, err error) {
	byteReader, ok := hr.r.(io.ByteReader)
	b := make([]byte, 1)

	if ok {
		c, err = byteReader.ReadByte()
		if err == nil {
			b[0] = c
			hr.h.Write(b)
		}
		return c, err
	} else {
		_, err := hr.Read(b)
		return b[0], err
	}
}

func (hr *HashingReader) Reset() {
	hr.h.Reset()
}

func (hr *HashingReader) Sum(b []byte) []byte {
	return hr.h.Sum(b)
}
