// Copyright 2015 Eryx <evorui аt gmаil dοt cοm>, All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package inutil

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"
)

func Uint32ToHexString(v uint32) string {
	bs := make([]byte, 4)
	binary.BigEndian.PutUint32(bs, v)
	return hex.EncodeToString(bs)
}

// SeqRandHexString generates a timestamp-prefixed random hex string.
func SeqRandHexString(slen, rlen int) string {
	if m := slen % 2; m > 0 {
		slen += 1
	}
	if slen < 2 {
		slen = 2
	} else if slen > 8 {
		slen = 8
	}
	if m := rlen % 2; m > 0 {
		rlen += 1
	}
	if rlen < 2 {
		rlen = 2
	} else if rlen > 1204 {
		rlen = 1024
	}
	id := Uint32ToHexString(uint32(time.Now().Unix()))
	if slen < 8 {
		id = id[:slen]
	}
	return id + RandHexString(rlen/2)
}

// RandHexString generates a random hexadecimal string of n bytes (length = n*2).
func RandHexString(n int) string {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return ""
	}
	return hex.EncodeToString(bytes)
}

// GenerateSecretKey generates a random hex key of byteSize bytes.
func GenerateSecretKey(byteSize int) (string, error) {
	b := make([]byte, byteSize)
	_, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// GenerateSecretKeyBase64 generates a random base64-encoded key.
func GenerateSecretKeyBase64(byteSize int) (string, error) {
	b := make([]byte, byteSize)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

const base62Chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// GenerateSecretKeyBase62 generates a random base62-encoded key.
func GenerateSecretKeyBase62(length int) (string, error) {
	result := make([]byte, length)
	maxInt := big.NewInt(int64(len(base62Chars)))

	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, maxInt)
		if err != nil {
			return "", err
		}
		result[i] = base62Chars[n.Int64()]
	}

	return string(result), nil
}
