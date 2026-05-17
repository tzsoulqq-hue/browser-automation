package app

import (
	"crypto/rand"
	"encoding/hex"
)

type RandomIDGenerator struct{}

func (RandomIDGenerator) NewID(prefix string) string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(err)
	}
	return prefix + hex.EncodeToString(buf[:])
}
