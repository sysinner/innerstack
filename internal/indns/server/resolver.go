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

package server

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"

	"github.com/sysinner/innerstack/v2/internal/indns/config"
)

type Resolver struct {
	mu          sync.RWMutex
	servers     map[string]bool
	nameservers []string
	caches      map[string]*ResolverRecordEntry
	hit         uint64
	mis         uint64
	err         uint64
}

type ResolverRecordEntry struct {
	msg *dns.Msg
	ttl uint32
}

var (
	resolvFiles = []string{
		"/etc/resolv.conf",
	}
)

func NewResolver() *Resolver {
	//
	r := &Resolver{
		servers: map[string]bool{},
		caches:  map[string]*ResolverRecordEntry{},
	}
	//
	for _, v := range config.Config.Server.NameServers {
		if err := r.addServer(v); err != nil {
			slog.Warn("add nameserver failed", "error", err)
		}
	}
	//
	for _, v := range resolvFiles {
		if err := r.parseFile(v); err != nil {
			slog.Info("parse resolv file", "error", err)
		}
	}
	//
	return r
}

func (it *Resolver) addServer(nameserver string) error {

	var (
		port = 53
		ipp  = strings.Split(nameserver, ":")
	)

	if len(ipp) == 2 {
		if v, e := strconv.Atoi(ipp[1]); e == nil && v > 0 && v < 65536 {
			port = v
		}
	}

	ip := net.ParseIP(ipp[0])
	if ip == nil {
		return fmt.Errorf("invalid address %s", nameserver)
	}

	addr := fmt.Sprintf("%s:%d", ip.String(), port)

	if _, ok := it.servers[addr]; !ok {
		it.servers[addr] = true
		it.nameservers = append(it.nameservers, addr)
		slog.Info("nameserver added", "addr", addr)
	}

	return nil
}

func (it *Resolver) parseFile(path string) error {

	it.mu.Lock()
	defer it.mu.Unlock()

	bs, err := readFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(bs), "\n")

	for _, line := range lines {

		line = strings.Replace(strings.TrimSpace(line), "\t", " ", -1)

		if !strings.HasPrefix(line, "nameserver") {
			continue
		}

		ar := strings.Split(line, " ")
		if len(ar) != 2 {
			continue
		}

		if err := it.addServer(ar[1]); err != nil {
			slog.Warn("add nameserver from file failed", "error", err)
		}
	}

	return nil
}

func (it *Resolver) Lookup(network string, req *dns.Msg) (*dns.Msg, error) {

	if len(it.nameservers) < 1 {
		return nil, errors.New("no nameserver found")
	}

	var (
		qName = req.Question[0].Name
		hit   *dns.Msg
		tn    = uint32(time.Now().Unix())
	)

	if it.hit > 0 && ((it.hit+it.mis+it.err)%10000) == 0 {
		slog.Info("resolver stats", "records", len(it.caches), "hit", it.hit, "mis", it.mis, "err", it.err)
	}

	{
		it.mu.RLock()
		if p, ok := it.caches[qName]; ok && p.ttl > tn {
			hit = p.msg
		}
		it.mu.RUnlock()

		if hit != nil {
			msg := *hit
			msg.Id = req.Id
			atomic.AddUint64(&it.hit, 1)
			return &msg, nil
		}
	}

	var (
		c = &dns.Client{
			Net:          network,
			ReadTimeout:  netTimeout,
			WriteTimeout: netTimeout,
		}
		rsq = make(chan *dns.Msg, len(it.nameservers))
	)

	lookupAction := func(nameserver string) {
		msg, _, err := c.Exchange(req, nameserver)
		if err == nil && msg != nil && msg.Rcode == dns.RcodeSuccess {
			rsq <- msg
		}
	}

	for _, v := range it.nameservers {

		go lookupAction(v)

		select {
		case hit = <-rsq:

		case <-time.After(500 * time.Millisecond):
		}

		if hit != nil {
			break
		}
	}

	if hit == nil {
		select {
		case hit = <-rsq:

		case <-time.After(netTimeout):
		}

		if hit == nil {
			atomic.AddUint64(&it.err, 1)
			return nil, errors.New("timeout")
		}
	}

	ttl := uint32(10)
	for _, v := range hit.Answer {
		if v != nil && v.Header().Ttl > 0 && v.Header().Ttl < ttl {
			ttl = v.Header().Ttl
		}
	}
	if ttl < 600 {
		ttl = 600
	} else if ttl > 86400 {
		ttl = 86400
	}
	ttl += uint32(time.Now().Unix())

	it.mu.Lock()
	it.caches[qName] = &ResolverRecordEntry{
		msg: hit,
		ttl: ttl,
	}
	it.mu.Unlock()

	atomic.AddUint64(&it.mis, 1)

	return hit, nil
}

func readFile(file string) ([]byte, error) {

	fp, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer fp.Close()

	return ioutil.ReadAll(fp)
}
