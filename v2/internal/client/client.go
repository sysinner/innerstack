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

package client

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	hauth1 "github.com/hooto/hauth/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/lynkdb/lynkapi/go/lynkapi"

	"github.com/lynkdb/kvgo/v2/pkg/kvapi"
)

const (
	grpcMsgByteMax = 12 << 20
)

var (
	rpcClientConns = map[string]*grpc.ClientConn{}
	rpcClientMu    sync.Mutex

	dbMut sync.Mutex
	// map : address + key.id + [database] -> client
	dbConns = map[string]*clientConn{}

	// map : address + key.id -> client
	admConns = map[string]*adminClientConn{}
)

type Config struct {
	Addr      string               `toml:"addr" json:"addr"`
	Database  string               `toml:"database,omitempty" json:"database,omitempty"`
	AccessKey *hauth1.AccessKey    `toml:"access_key" json:"access_key"`
	Options   *kvapi.ClientOptions `toml:"options,omitempty" json:"options,omitempty"`
}

type clientConn struct {
	_ak      string
	cfg      *Config
	rpcConn  *grpc.ClientConn
	database string
	kvClient kvapi.KvgoClient
	err      error
}

type adminClientConn struct {
	_ak string
	cfg *lynkapi.ClientConfig
	ac  lynkapi.Client
	err error
}

func (it *Config) NewClient() (kvapi.Client, error) {

	if it.AccessKey == nil {
		return nil, errors.New("access key not setup")
	}

	ak := fmt.Sprintf("%s.%s", it.Addr, it.AccessKey.Id)

	dbMut.Lock()
	defer dbMut.Unlock()

	if dbConns == nil {
		dbConns = map[string]*clientConn{}
	}

	dbConn, ok := dbConns[ak]
	if !ok {

		conn, err := rpcClientConnect(it.Addr, it.AccessKey, false)
		if err != nil {
			return nil, err
		}

		if it.Options == nil {
			it.Options = kvapi.DefaultClientOptions()
		}

		dbConn = &clientConn{
			_ak:      ak,
			cfg:      it,
			rpcConn:  conn,
			kvClient: kvapi.NewKvgoClient(conn),
		}
		dbConns[ak] = dbConn
	}

	return dbConn.setDatabase(it.Database), nil
}

func (it *Config) NewAdminClient() (lynkapi.Client, error) {

	if it.AccessKey == nil {
		return nil, errors.New("access key not setup")
	}

	ak := fmt.Sprintf("%s.%s", it.Addr, it.AccessKey.Id)

	dbMut.Lock()
	defer dbMut.Unlock()

	if admConns == nil {
		admConns = map[string]*adminClientConn{}
	}

	admConn, ok := admConns[ak]
	if !ok {

		c := &lynkapi.ClientConfig{
			Addr:      it.Addr,
			AccessKey: it.AccessKey,
		}

		ac, err := c.NewClient()
		if err != nil {
			return nil, err
		}

		admConn = &adminClientConn{
			_ak: ak,
			cfg: c,
			ac:  ac,
		}
		admConns[ak] = admConn
	}

	return admConn.ac, nil
}

func (it *Config) timeout() time.Duration {
	if it.Options == nil {
		it.Options = kvapi.DefaultClientOptions()
	}
	return time.Millisecond * time.Duration(it.Options.Timeout)
}

// func (it *clientConn) tryConnect(retry bool) error {
//      if it.rpcConn == nil {
//              conn, err := rpcClientConnect(it.cfg.Addr, it.cfg.AccessKey, true)
//              if err != nil {
//                      return err
//              }
//              it.rpcConn = conn
//              it.kvClient  = kvapi.NewKvgoClient(conn)
//      }
//      return nil
// }

func (it *clientConn) Read(req *kvapi.ReadRequest) *kvapi.ResultSet {

	// if err := it.tryConnect(false); err != nil {
	//      return newResultSetWithClientError(err.Error())
	// }

	ctx, fc := context.WithTimeout(context.Background(), it.cfg.timeout())
	defer fc()

	if req.Database == "" {
		req.Database = it.database
	}

	rs, err := it.kvClient.Read(ctx, req)
	if err != nil {
		return newResultSetWithClientError(err.Error())
	}

	return rs
}

func (it *clientConn) Range(req *kvapi.RangeRequest) *kvapi.ResultSet {

	ctx, fc := context.WithTimeout(context.Background(), it.cfg.timeout())
	defer fc()

	if req.Database == "" {
		req.Database = it.database
	}

	rs, err := it.kvClient.Range(ctx, req)
	if err != nil {
		return newResultSetWithClientError(err.Error())
	}

	return rs
}

func (it *clientConn) Write(req *kvapi.WriteRequest) *kvapi.ResultSet {

	ctx, fc := context.WithTimeout(context.Background(), it.cfg.timeout())
	defer fc()

	if req.Database == "" {
		req.Database = it.database
	}

	rs, err := it.kvClient.Write(ctx, req)
	if err != nil {
		return newResultSetWithClientError(err.Error())
	}

	return rs
}

func (it *clientConn) Delete(req *kvapi.DeleteRequest) *kvapi.ResultSet {

	ctx, fc := context.WithTimeout(context.Background(), it.cfg.timeout())
	defer fc()

	if req.Database == "" {
		req.Database = it.database
	}

	rs, err := it.kvClient.Delete(ctx, req)
	if err != nil {
		return newResultSetWithClientError(err.Error())
	}

	return rs
}

func (it *clientConn) Batch(req *kvapi.BatchRequest) *kvapi.BatchResponse {

	ctx, fc := context.WithTimeout(context.Background(), it.cfg.timeout())
	defer fc()

	rs, err := it.kvClient.Batch(ctx, req)
	if err != nil {
		return &kvapi.BatchResponse{
			StatusCode:    kvapi.Status_RequestTimeout,
			StatusMessage: err.Error(),
		}
	}

	return rs
}

func (it *clientConn) setDatabase(name string) kvapi.Client {
	if name == "" || name == it.database {
		return it
	}
	dbConn, ok := dbConns[it._ak+":"+name]
	if ok {
		return dbConn
	}
	dbConn = &clientConn{
		_ak:      it._ak,
		database: name,
		cfg:      it.cfg,
		rpcConn:  it.rpcConn,
		kvClient: it.kvClient,
	}
	dbConns[it._ak+":"+name] = dbConn
	return dbConn
}

func (it *clientConn) SetDatabase(name string) kvapi.Client {
	dbMut.Lock()
	defer dbMut.Unlock()
	return it.setDatabase(name)
}

func (it *clientConn) NewReader(key []byte, keys ...[]byte) kvapi.ClientReader {
	r := &clientReader{
		cc:  it,
		req: kvapi.NewReadRequest(key, keys...).SetDatabase(it.database),
	}
	return r
}

func (it *clientConn) NewRanger(lowerKey, upperKey []byte) kvapi.ClientRanger {
	r := &clientRanger{
		cc:  it,
		req: kvapi.NewRangeRequest(lowerKey, upperKey).SetDatabase(it.database),
	}
	return r
}

func (it *clientConn) NewWriter(key []byte, value interface{}) kvapi.ClientWriter {
	r := &clientWriter{
		cc:  it,
		req: kvapi.NewWriteRequest(key, value).SetDatabase(it.database),
	}
	return r
}

func (it *clientConn) NewDeleter(key []byte) kvapi.ClientDeleter {
	r := &clientDeleter{
		cc:  it,
		req: kvapi.NewDeleteRequest(key).SetDatabase(it.database),
	}
	return r
}

func (it *clientConn) Flush() error {
	return nil
}

func (it *clientConn) Close() error {
	if it.rpcConn != nil {
		return it.rpcConn.Close()
	}
	return nil
}

type clientReader struct {
	cc  *clientConn
	req *kvapi.ReadRequest
}

func (it *clientReader) SetMetaOnly(b bool) kvapi.ClientReader {
	it.req.SetMetaOnly(b)
	return it
}

func (it *clientReader) SetAttrs(attrs uint64) kvapi.ClientReader {
	it.req.SetAttrs(attrs)
	return it
}

func (it *clientReader) Exec() *kvapi.ResultSet {
	return it.cc.Read(it.req)
}

type clientRanger struct {
	cc  *clientConn
	req *kvapi.RangeRequest
}

func (it *clientRanger) SetLimit(n int64) kvapi.ClientRanger {
	it.req.SetLimit(n)
	return it
}

func (it *clientRanger) SetRevert(b bool) kvapi.ClientRanger {
	it.req.SetRevert(b)
	return it
}

func (it *clientRanger) Exec() *kvapi.ResultSet {
	return it.cc.Range(it.req)
}

type clientWriter struct {
	cc  *clientConn
	req *kvapi.WriteRequest
}

func (it *clientWriter) SetTTL(ttl int64) kvapi.ClientWriter {
	it.req.SetTTL(ttl)
	return it
}

func (it *clientWriter) SetAttrs(attrs uint64) kvapi.ClientWriter {
	it.req.SetAttrs(attrs)
	return it
}

func (it *clientWriter) SetIncr(id uint64, ns string) kvapi.ClientWriter {
	it.req.SetIncr(id, ns)
	return it
}

func (it *clientWriter) SetJsonValue(o interface{}) kvapi.ClientWriter {
	it.req.SetValueEncode(o, kvapi.JsonValueCodec)
	return it
}

func (it *clientWriter) SetCreateOnly(b bool) kvapi.ClientWriter {
	it.req.CreateOnly = b
	return it
}

func (it *clientWriter) SetPrevVersion(v uint64) kvapi.ClientWriter {
	it.req.PrevVersion = v
	return it
}

func (it *clientWriter) SetPrevChecksum(v interface{}) kvapi.ClientWriter {
	it.req.SetPrevChecksum(v)
	return it
}

func (it *clientWriter) Exec() *kvapi.ResultSet {
	if it.req.Database == "" {
		return it.cc.Write(it.req.SetDatabase(it.cc.database))
	}
	return it.cc.Write(it.req)
}

type clientDeleter struct {
	cc  *clientConn
	req *kvapi.DeleteRequest
}

func (it *clientDeleter) SetRetainMeta(b bool) kvapi.ClientDeleter {
	it.req.SetRetainMeta(b)
	return it
}

func (it *clientDeleter) SetPrevVersion(v uint64) kvapi.ClientDeleter {
	it.req.PrevVersion = v
	return it
}

func (it *clientDeleter) SetPrevChecksum(v interface{}) kvapi.ClientDeleter {
	it.req.SetPrevChecksum(v)
	return it
}

func (it *clientDeleter) Exec() *kvapi.ResultSet {
	return it.cc.Delete(it.req)
}

func rpcClientConnect(addr string,
	key *hauth1.AccessKey,
	forceNew bool) (*grpc.ClientConn, error) {

	if key == nil {
		return nil, errors.New("not auth key setup")
	}

	ck := fmt.Sprintf("%s.%s", addr, key.Id)

	rpcClientMu.Lock()
	defer rpcClientMu.Unlock()

	if c, ok := rpcClientConns[ck]; ok {
		if forceNew {
			c.Close()
			c = nil
			delete(rpcClientConns, ck)
		} else {
			return c, nil
		}
	}

	dialOptions := []grpc.DialOption{
		grpc.WithPerRPCCredentials(newAppCredential(key)),
		grpc.WithMaxMsgSize(grpcMsgByteMax * 2),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(grpcMsgByteMax * 2)),
		grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(grpcMsgByteMax * 2)),
	}

	dialOptions = append(dialOptions, grpc.WithInsecure())

	c, err := grpc.Dial(addr, dialOptions...)
	if err != nil {
		return nil, err
	}

	rpcClientConns[ck] = c

	return c, nil
}

func newAppCredential(key *hauth1.AccessKey) credentials.PerRPCCredentials {
	return hauth1.NewGrpcAppCredential(key)
}
