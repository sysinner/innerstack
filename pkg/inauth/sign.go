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

package inauth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"sync"
)

type Signer interface {
	Name() string
	Sign(signingString string, key any) ([]byte, error)
}

type SignerManager struct {
	mu    sync.RWMutex
	items map[string]Signer
}

var Signers = SignerManager{
	items: map[string]Signer{},
}

var DefaultSigner = &hmacSigner{"HS256", crypto.SHA256}

func init() {

	// https://datatracker.ietf.org/doc/html/rfc7518#section-3.1

	Signers.Register(&hmacSigner{"HS256", crypto.SHA256}) // Required
	Signers.Register(&hmacSigner{"HS512", crypto.SHA512})

	Signers.Register(&rsaSigner{"RS256", crypto.SHA256}) // Recommended
	Signers.Register(&rsaSigner{"RS512", crypto.SHA512})

	Signers.Register(&ecdsaSigner{"ES256", crypto.SHA256, 32, 256}) // Recommended+
	Signers.Register(&ecdsaSigner{"ES512", crypto.SHA512, 66, 521})
}

func (it *SignerManager) Register(s Signer) {
	it.mu.Lock()
	defer it.mu.Unlock()
	it.items[s.Name()] = s
}

func (it *SignerManager) Signer(name string) Signer {
	it.mu.RLock()
	defer it.mu.RUnlock()
	if s, ok := it.items[name]; ok {
		return s
	}
	return &noneSigner{}
}

func Sign(header TokenHeader, claims any, key any) (string, error) {

	header.Alg = DefaultSigner.Name()

	signingString := bytesEncode(jsonEncode(header)) + "." +
		bytesEncode(jsonEncode(claims))

	bs, err := DefaultSigner.Sign(signingString, key)
	if err != nil {
		return "", nil
	}

	signString := bytesEncode(bs)

	return signingString + "." + signString, nil
}

type noneSigner struct{}

func (it noneSigner) Name() string {
	return "none"
}

func (it noneSigner) Sign(signingString string, key any) ([]byte, error) {
	return nil, errors.New("none signer")
}

type hmacSigner struct {
	name string
	hash crypto.Hash
}

func (it hmacSigner) Name() string {
	return it.name
}

func (it hmacSigner) Sign(signingString string, key any) ([]byte, error) {

	keyBytes, ok := key.([]byte)
	if !ok {
		keyStr, ok := key.(string)
		if !ok {
			return nil, errors.New("invalid key (type:[]byte)")
		}
		keyBytes = []byte(keyStr)
	}

	hasher := hmac.New(it.hash.New, keyBytes)
	hasher.Write([]byte(signingString))

	signBytes := hasher.Sum(nil)

	return signBytes, nil
}

type rsaSigner struct {
	name string
	hash crypto.Hash
}

func (it rsaSigner) Name() string {
	return it.name
}

func (it rsaSigner) Sign(signingString string, key any) ([]byte, error) {

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("invalid key (type:rsa)")
	}

	hasher := it.hash.New()
	hasher.Write([]byte(signingString))

	signBytes, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, it.hash, hasher.Sum(nil))
	if err != nil {
		return nil, err
	}

	return signBytes, nil
}

type ecdsaSigner struct {
	name      string
	hash      crypto.Hash
	keySize   int
	curveBits int
}

func (it ecdsaSigner) Name() string {
	return it.name
}

func (it ecdsaSigner) Sign(signingString string, key any) ([]byte, error) {
	ecdsaKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("invalid key (type:ecdsa)")
	}

	hasher := it.hash.New()
	hasher.Write([]byte(signingString))

	r, s, err := ecdsa.Sign(rand.Reader, ecdsaKey, hasher.Sum(nil))
	if err != nil {
		return nil, err
	}

	curveBits := ecdsaKey.Curve.Params().BitSize
	if it.curveBits != curveBits {
		return nil, errors.New("invalid key (type:ecdsa)")
	}

	keyBytes := curveBits / 8
	if (curveBits % 8) > 0 {
		keyBytes += 1
	}

	signBytes := make([]byte, 2*keyBytes)

	r.FillBytes(signBytes[0:keyBytes])
	s.FillBytes(signBytes[keyBytes:])

	return signBytes, nil
}
