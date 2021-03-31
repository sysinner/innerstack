// Copyright 2015 lynkdb Authors, All rights reserved.
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

package utils

import (
	"encoding/binary"
	"encoding/hex"
)

func HexStringToBytes(s string) []byte {
	if s != "" {
		if dec, err := hex.DecodeString(s); err == nil {
			return dec
		}
	}
	return []byte{}
}

func BytesToHexString(bs []byte) string {
	return hex.EncodeToString(bs)
}

func BytesToUint64(bs []byte) uint64 {

	if len(bs) != 8 {
		return 0
	}

	return binary.BigEndian.Uint64(bs)
}

func Uint64ToBytes(v uint64) []byte {

	bs := make([]byte, 8)
	binary.BigEndian.PutUint64(bs, v)

	return bs
}

func BytesToUint32(bs []byte) uint32 {

	if len(bs) != 4 {
		return 0
	}

	return binary.BigEndian.Uint32(bs)
}

func Uint32ToBytes(v uint32) []byte {

	bs := make([]byte, 4)
	binary.BigEndian.PutUint32(bs, v)

	return bs
}

func Uint32ToHexString(v uint32) string {
	return BytesToHexString(Uint32ToBytes(v))
}

func Uint64ToHexString(v uint64) string {
	return BytesToHexString(Uint64ToBytes(v))
}
