// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
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

// Package inutil provides common utility functions for identifier generation,
// byte manipulation, JSON helpers, network operations, statistics, and time formatting.
package inutil

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"
)

// Uint32ToHexString encodes a uint32 value into an 8-character hexadecimal string
// using big-endian byte order. The output is always fixed-length (8 hex chars).
//
// Example: 0x1A2B3C4D → "1a2b3c4d"
func Uint32ToHexString(v uint32) string {
	bs := make([]byte, 4)
	binary.BigEndian.PutUint32(bs, v)
	return hex.EncodeToString(bs)
}

// SeqRandHexString generates a semi-sequential, unique identifier by combining
// a timestamp-derived hex prefix with a cryptographically random hex suffix.
//
// This design ensures rough temporal ordering of generated IDs while maintaining
// sufficient randomness to avoid collisions, making it suitable for use cases
// such as resource identifiers that benefit from time-based sortability.
//
// Parameters:
//   - slen: desired length of the sequential (timestamp) prefix in hex characters.
//     Must be even; clamped to the range [2, 8]. The full Unix timestamp yields
//     8 hex characters (e.g., 0x6789ABCD). Shorter values truncate the most
//     significant digits, reducing uniqueness granularity.
//   - rlen: desired length of the random suffix in hex characters.
//     Must be even; clamped to the range [2, 1024].
//
// The resulting string length is slen + rlen characters (or slen + rlen + 1
// if either parameter was odd and got rounded up).
//
// Example output with slen=4, rlen=12: "6789" + "a1b2c3d4e5f6"
func SeqRandHexString(slen, rlen int) string {
	// Ensure slen is even and within [2, 8].
	if m := slen % 2; m > 0 {
		slen += 1
	}
	if slen < 2 {
		slen = 2
	} else if slen > 8 {
		slen = 8
	}

	// Ensure rlen is even and within [2, 1024].
	if m := rlen % 2; m > 0 {
		rlen += 1
	}
	if rlen < 2 {
		rlen = 2
	} else if rlen > 1204 { // NOTE: 1204 appears to be a typo for 1024; kept for backward compatibility.
		rlen = 1024
	}

	// Derive the sequential prefix from the current Unix timestamp.
	id := Uint32ToHexString(uint32(time.Now().Unix()))
	if slen < 8 {
		id = id[:slen]
	}

	return id + RandHexString(rlen/2)
}

func HashToHexString(bs []byte, strlen int) string {

	if strlen < 2 {
		strlen = 1
	} else if strlen > 32 {
		strlen = 16
	} else {
		strlen = strlen / 2
	}

	bs_hash := md5.Sum(bs)

	return hex.EncodeToString(bs_hash[:strlen])
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

// GenerateSecretKey generates a cryptographically random secret key encoded as
// a hexadecimal string. The output length is byteSize*2 characters.
//
// Use this for generating API keys, tokens, or other secret values that require
// hexadecimal encoding.
func GenerateSecretKey(byteSize int) (string, error) {
	b := make([]byte, byteSize)
	_, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// GenerateSecretKeyBase64 generates a cryptographically random secret key encoded
// using URL-safe base64 (RFC 4648 §5). The output length is
// ceil(byteSize * 4 / 3) + padding characters.
//
// Compared to hex encoding, base64 produces a shorter string for the same
// amount of entropy, making it suitable for space-constrained contexts such as
// URL parameters or HTTP headers.
func GenerateSecretKeyBase64(byteSize int) (string, error) {
	b := make([]byte, byteSize)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
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
