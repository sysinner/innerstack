// Copyright 2019 Eryx <evorui аt gmаil dοt cοm>, All rights reserved.
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

package kvspec

// StorageOptions holds the optional parameters for the DB storage engine
type StorageOptions struct {

	// WriteBufferSize defines maximum memory size of the journal before flushed to disk.
	//
	// The default value is 8 MiB.
	WriteBufferSize int `toml:"write_buffer_size" json:"write_buffer_size" desc:"in MiB, default to 8"`

	// BlockCacheSize defines the capacity of the 'sorted table' block caching.
	//
	// The default value is 8 MiB.
	BlockCacheSize int `toml:"block_cache_size" json:"block_cache_size" desc:"in MiB, default to 8"`

	// MaxTableSize limits size of 'sorted table' that compaction generates.
	//
	// The default value is 8 MiB.
	MaxTableSize int `toml:"max_table_size" json:"max_table_size" desc:"in MiB, default to 8"`

	// MaxOpenFiles defines the capacity of the open files caching.
	//
	// The default value is 500.
	MaxOpenFiles int `toml:"max_open_files" json:"max_open_files" desc:"default to 500"`

	// TableCompressName defines the 'sorted table' block compression to use.
	//
	// The default value is 'snappy'.
	TableCompressName string `toml:"table_compress_name" json:"table_compress_name" desc:"default to snappy"`
}

// StorageResult defines DB reply methods.
type StorageResult interface {
	// Returns true iff the status indicates success.
	OK() bool

	// Bytes returns error.
	Error() error

	// Bytes returns error string message.
	ErrorMessage() string

	// Returns true if the key not found.
	NotFound() bool

	// Len returns data size.
	Len() int

	// Bytes returns data in bytes.
	Bytes() []byte

	// String returns data in string.
	String() string
}

type StorageReadOptions struct{}

type StorageWriteOptions struct{}

type StorageDeleteOptions struct{}

// StorageIteratorRange is a key range.
type StorageIteratorRange struct {
	Start []byte
	Limit []byte
}

// StorageBatch defines a write batch methods.
type StorageBatch interface {
	// Put appends 'put operation' of the given key/value pair to the batch.
	Put(key, value []byte)

	// Delete appends 'delete operation' of the given key to the batch.
	Delete(key []byte)

	// Len returns number of records in the batch.
	Len() int

	// Reset resets the batch.
	Reset()

	// Commit apply the given batch to the DB. The batch records will be applied
	// sequentially.
	Commit() error
}

// StorageEngine defines the basic engine methods.
type StorageEngine interface {
	// Put sets the value for the given key. It overwrites any previous value
	// for that key.
	Put(key, value []byte, opts *StorageWriteOptions) StorageResult

	// Get gets the value for the given key.
	Get(key []byte, opts *StorageReadOptions) StorageResult

	// Delete deletes the value for the given key. Delete will not returns error if
	// key doesn't exist.
	Delete(key []byte, opts *StorageDeleteOptions) StorageResult

	// NewBatch returns empty preallocated batch.
	NewBatch() StorageBatch

	// NewIterator returns an iterator for the latest snapshot of the
	// underlying DB.
	NewIterator(r *StorageIteratorRange) StorageIterator

	// SizeOf calculates approximate sizes of the given key ranges.
	SizeOf(args []*StorageIteratorRange) ([]int64, error)

	// Close closes the DB. This will also releases any outstanding snapshot,
	// abort any in-flight compaction and discard open transaction.
	Close() error
}

// StorageIterator is the interface that wraps iterator methods.
type StorageIterator interface {
	// Valid iterator is either positioned at a key/value pair, or
	// not valid.  This method returns true iff the iterator is valid.
	Valid() bool

	// First moves the iterator to the first key/value pair. If the iterator
	// only contains one key/value pair then First and Last would moves
	// to the same key/value pair.
	// It returns whether such pair exist.
	First() bool

	// Last moves the iterator to the last key/value pair. If the iterator
	// only contains one key/value pair then First and Last would moves
	// to the same key/value pair.
	// It returns whether such pair exist.
	Last() bool

	// Seek moves the iterator to the first key/value pair whose key is greater
	// than or equal to the given key.
	// It returns whether such pair exist.
	//
	// It is safe to modify the contents of the argument after Seek returns.
	Seek(key []byte) bool

	// Next moves the iterator to the next key/value pair.
	// It returns false if the iterator is exhausted.
	Next() bool

	// Prev moves the iterator to the previous key/value pair.
	// It returns false if the iterator is exhausted.
	Prev() bool

	// Key returns the key of the current key/value pair
	Key() []byte

	// Value returns the value of the current key/value pair
	Value() []byte

	// Error returns any accumulated error.
	Error() error

	// Release releases associated resources. Release should always success
	// and can be called multiple times without causing error.
	Release()
}

type StorageEngineOpen func(path string, opts *StorageOptions) (StorageEngine, error)

func (it *StorageOptions) Reset() *StorageOptions {

	if it.WriteBufferSize == 0 {
		it.WriteBufferSize = 8
	} else if it.WriteBufferSize < 2 {
		it.WriteBufferSize = 2
	} else if it.WriteBufferSize > 256 {
		it.WriteBufferSize = 256
	}

	if it.BlockCacheSize == 0 {
		it.BlockCacheSize = 8
	} else if it.BlockCacheSize < 2 {
		it.BlockCacheSize = 2
	} else if it.BlockCacheSize > 64 {
		it.BlockCacheSize = 64
	}

	if it.MaxTableSize == 0 {
		it.MaxTableSize = 8
	} else if it.MaxTableSize < 2 {
		it.MaxTableSize = 2
	} else if it.MaxTableSize > 64 {
		it.MaxTableSize = 64
	}

	if it.MaxOpenFiles == 0 {
		it.MaxOpenFiles = 500
	} else if it.MaxOpenFiles < 100 {
		it.MaxOpenFiles = 100
	} else if it.MaxOpenFiles > 10000 {
		it.MaxOpenFiles = 10000
	}

	if it.TableCompressName != "none" {
		it.TableCompressName = "snappy"
	}

	return it
}
