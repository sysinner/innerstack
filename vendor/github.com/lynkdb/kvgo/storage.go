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

package kvgo

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/hooto/hlog4g/hlog"
	"github.com/syndtr/goleveldb/leveldb"
	lerrors "github.com/syndtr/goleveldb/leveldb/errors"
	"github.com/syndtr/goleveldb/leveldb/filter"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"

	kv2 "github.com/lynkdb/kvspec/go/kvspec/v2"
)

var (
	StorageEngineOpen kv2.StorageEngineOpen = leveldbStorageOpen
)

func leveldbStorageOpen(path string, opts *kv2.StorageOptions) (kv2.StorageEngine, error) {

	dir := filepath.Clean(path)

	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, err
	}

	opts = opts.Reset()

	ldbOpts := &opt.Options{
		WriteBuffer:            opts.WriteBufferSize * opt.MiB,
		BlockCacheCapacity:     opts.BlockCacheSize * opt.MiB,
		CompactionTableSize:    opts.MaxTableSize * opt.MiB,
		OpenFilesCacheCapacity: opts.MaxOpenFiles,
		Filter:                 filter.NewBloomFilter(10),
	}

	if opts.TableCompressName == "snappy" {
		ldbOpts.Compression = opt.SnappyCompression
	} else {
		ldbOpts.Compression = opt.NoCompression
	}

	db, err := leveldb.OpenFile(dir, ldbOpts)
	if err != nil {
		return nil, err
	}

	if jbs, err := json.Marshal(opts); err == nil {
		hlog.Printf("info", "db %s, opts %s", path, string(jbs))
	}

	return &leveldbStorageEngine{
		db: db,
	}, nil
}

type leveldbStorageEngine struct {
	db *leveldb.DB
}

func (it *leveldbStorageEngine) Put(key, value []byte,
	opts *kv2.StorageWriteOptions) kv2.StorageResult {
	return newLeveldbStorageResultError(it.db.Put(key, value, nil))
}

func (it *leveldbStorageEngine) Get(key []byte,
	opts *kv2.StorageReadOptions) kv2.StorageResult {
	return newLeveldbStorageResult(it.db.Get(key, nil))
}

func (it *leveldbStorageEngine) Delete(key []byte,
	opts *kv2.StorageDeleteOptions) kv2.StorageResult {
	return newLeveldbStorageResultError(it.db.Delete(key, nil))
}

func (it *leveldbStorageEngine) NewBatch() kv2.StorageBatch {
	b := &leveldbStorageBatch{
		batch: new(leveldb.Batch),
		db:    it.db,
	}
	b.batch.Reset()
	return b
}

func (it *leveldbStorageEngine) NewIterator(opts *kv2.StorageIteratorRange) kv2.StorageIterator {
	return newLeveldbStorageIterator(it.db, opts)
}

func (it *leveldbStorageEngine) SizeOf(args []*kv2.StorageIteratorRange) ([]int64, error) {
	if len(args) == 0 {
		return nil, nil
	}
	opts := []util.Range{}
	for _, v := range args {
		opts = append(opts, util.Range{Start: v.Start, Limit: v.Limit})
	}
	return it.db.SizeOf(opts)
}

func (it *leveldbStorageEngine) Close() error {
	return it.db.Close()
}

type leveldbStorageResult struct {
	bytes []byte
	err   error
}

func newLeveldbStorageResult(bs []byte, err error) *leveldbStorageResult {
	return &leveldbStorageResult{
		bytes: bs,
		err:   err,
	}
}

func newLeveldbStorageResultError(err error) *leveldbStorageResult {
	return &leveldbStorageResult{
		err: err,
	}
}

func (it *leveldbStorageResult) OK() bool {
	return it.err == nil
}

func (it *leveldbStorageResult) Error() error {
	return it.err
}

func (it *leveldbStorageResult) ErrorMessage() string {
	if it.err != nil {
		return it.err.Error()
	}
	return ""
}

func (it *leveldbStorageResult) NotFound() bool {
	return (it.err != nil && it.err == lerrors.ErrNotFound)
}

func (it *leveldbStorageResult) Bytes() []byte {
	return it.bytes
}

func (it *leveldbStorageResult) Len() int {
	return len(it.bytes)
}

func (it *leveldbStorageResult) String() string {
	return string(it.bytes)
}

type leveldbStorageBatch struct {
	batch *leveldb.Batch
	db    *leveldb.DB
}

func (it *leveldbStorageBatch) Put(key, value []byte) {
	it.batch.Put(key, value)
}

func (it *leveldbStorageBatch) Delete(key []byte) {
	it.batch.Delete(key)
}

func (it *leveldbStorageBatch) Len() int {
	return it.batch.Len()
}

func (it *leveldbStorageBatch) Reset() {
	it.batch.Reset()
}

func (it *leveldbStorageBatch) Commit() error {
	return it.db.Write(it.batch, nil)
}

type leveldbStorageIterator struct {
	iterator.Iterator
}

func newLeveldbStorageIterator(db *leveldb.DB,
	opts *kv2.StorageIteratorRange) *leveldbStorageIterator {
	return &leveldbStorageIterator{
		Iterator: db.NewIterator(&util.Range{
			Start: opts.Start,
			Limit: opts.Limit,
		}, nil),
	}
}

// func (it *leveldbStorageIterator) Error() error {
// 	return it.iter.Error()
// }

// func (it *leveldbStorageIterator) Release() {
// 	it.iter.Release()
// }

// func (it *leveldbStorageIterator) First() bool {
// 	return it.iter.First()
// }

// func (it *leveldbStorageIterator) Last() bool {
// 	return it.iter.Last()
// }

// func (it *leveldbStorageIterator) Seek(key []byte) bool {
// 	return it.iter.Seek(key)
// }

// func (it *leveldbStorageIterator) Prev() bool {
// 	return it.iter.Prev()
// }

// func (it *leveldbStorageIterator) Next() bool {
// 	return it.iter.Next()
// }

// func (it *leveldbStorageIterator) Key() []byte {
// 	return it.iter.Key()
// }

// func (it *leveldbStorageIterator) Value() []byte {
// 	return it.iter.Value()
// }
