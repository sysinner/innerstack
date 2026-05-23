// Copyright 2020 Eryx <evorui at gmail dot com>, All rights reserved.
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

package inauth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"

	"google.golang.org/protobuf/proto"
)

func jsonEncode(o any) []byte {
	if o == nil {
		return []byte("{}")
	}
	js, _ := json.Marshal(o)
	return js
}

func jsonDecode(b []byte, o any) error {
	return json.Unmarshal(b, o)
}

func bytesEncode(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func bytesDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

func ProtoEqual(obj1, obj2 proto.Message) bool {
	return proto.Equal(obj1, obj2)
}

func absInt64(a int64) int64 {
	if a < 0 {
		return -a
	}
	return a
}

// RandHexString generates a cryptographically random hexadecimal string using
// crypto/rand. The returned string length is n*2 characters (2 hex chars per byte).
//
// Returns an empty string if the random source fails (should be exceedingly rare).
func RandHexString(n int) string {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return ""
	}
	return hex.EncodeToString(bytes)
}

// base62Chars defines the character set for base62 encoding: [0-9a-zA-Z].
// This alphabet is URL-safe and human-readable, containing no special characters
// that might cause issues in identifiers or tokens.
const base62Chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// GenerateSecretKeyBase62 generates a cryptographically random key composed
// entirely of base62 characters (0-9, a-z, A-Z), producing a URL-safe,
// human-friendly identifier of the specified length.
//
// To ensure uniform distribution and eliminate modulo bias, only random bytes
// in the range [0, maxValidByte) are accepted. Since 256 % 62 = 8, the maximum
// valid byte value is 248, guaranteeing that each of the 62 characters has an
// exactly equal probability of being selected.
//
// An over-allocated random buffer (length/10 + 1 extra bytes) is used per
// iteration to minimize the number of syscalls to crypto/rand, as roughly
// 256-248 = 3.1% of random bytes will be rejected.
func GenerateSecretKeyBase62(length int) (string, error) {
	if length <= 0 {
		return "", nil
	}

	result := make([]byte, length)

	// Modulo bias elimination: 256 % 62 = 8, so the maximum unbiased byte
	// boundary is 248. Only bytes strictly below this threshold are mapped
	// to the 62-character alphabet, ensuring perfectly uniform distribution.
	const maxValidByte = 248

	// Allocate slightly more random bytes than needed to account for the
	// ~3.1% rejection rate of bytes >= maxValidByte.
	bufSize := length + (length / 10) + 1
	randomBytes := make([]byte, bufSize)

	for i := 0; i < length; {
		if _, err := rand.Read(randomBytes); err != nil {
			return "", err
		}

		for j := 0; j < len(randomBytes) && i < length; j++ {
			b := randomBytes[j]
			if b < maxValidByte {
				result[i] = base62Chars[b%62]
				i++
			}
		}
	}

	return string(result), nil
}
