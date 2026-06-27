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
	"log/slog"
	"net"
	"time"

	"github.com/miekg/dns"

	"github.com/sysinner/innerstack/v2/internal/indns/config"
)

type inServer struct {
	resolver *Resolver
}

var (
	netTimeout = 3 * time.Second
)

func Start() error {

	var (
		handler = inServer{
			resolver: NewResolver(),
		}
		err error
	)

	for _, nt := range []string{"udp", "tcp"} {

		netHandler := dns.NewServeMux()
		netHandler.HandleFunc(".", handler.action)

		netServer := &dns.Server{
			Addr:         config.Config.Server.Bind,
			Net:          nt,
			Handler:      netHandler,
			ReadTimeout:  netTimeout,
			WriteTimeout: netTimeout,
		}

		if nt == "udp" {
			netServer.UDPSize = 65535
			netServer.PacketConn, err = net.ListenPacket(netServer.Net, netServer.Addr)
		} else {
			netServer.Listener, err = net.Listen(netServer.Net, netServer.Addr)
		}

		if err != nil {
			return err
		}

		slog.Info("start listen ok", "network", netServer.Net, "addr", netServer.Addr)

		go netServer.ActivateAndServe()
	}

	return nil
}

func (h *inServer) action(w dns.ResponseWriter, req *dns.Msg) {

	if len(req.Question) < 1 || req.Question[0].Qtype != dns.TypeA {
		dns.HandleFailed(w, req)
		return
	}

	var (
		qName0  = req.Question[0].Name
		qName1  = unFqdn(qName0)
		reqAddr = w.RemoteAddr()
	)

	if ips := config.Records.Get(qName1); len(ips) > 0 {

		m := new(dns.Msg)
		m.SetReply(req)

		rrHeader := dns.RR_Header{
			Name:   qName0,
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    10,
		}

		for _, ip := range ips {
			a := &dns.A{rrHeader, ip}
			m.Answer = append(m.Answer, a)
		}

		w.WriteMsg(m)
		return
	}

	if mesg, err := h.resolver.Lookup(reqAddr.Network(), req); err == nil {
		w.WriteMsg(mesg)
		return
	}

	dns.HandleFailed(w, req)
}
