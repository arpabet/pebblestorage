/**
    Copyright (c) 2020-2022 Arpabet, Inc.

	Permission is hereby granted, free of charge, to any person obtaining a copy
	of this software and associated documentation files (the "Software"), to deal
	in the Software without restriction, including without limitation the rights
	to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
	copies of the Software, and to permit persons to whom the Software is
	furnished to do so, subject to the following conditions:

	The above copyright notice and this permission notice shall be included in
	all copies or substantial portions of the Software.

	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
	IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
	FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
	AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
	LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
	OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
	THE SOFTWARE.
*/

package pebblestorage

import (
	"bytes"
	"github.com/cockroachdb/pebble"
	"go.arpabet.com/storage"
	"go.arpabet.com/value"
	"io"
	"time"
)

type pebbleStorage struct {
	name  string
	db     *pebble.DB
}

func New(name string, conf *PebbleConfig) (storage.ManagedStorage, error) {

	db, err := OpenDatabase(conf)
	if err != nil {
		return nil, err
	}

	return &pebbleStorage {name: name, db: db}, nil
}

func FromDB(name string, db *pebble.DB) storage.ManagedStorage {
	return &pebbleStorage {name: name, db: db}
}

func (t* pebbleStorage) BeanName() string {
	return t.name
}

func (t* pebbleStorage) Destroy() error {
	return t.db.Close()
}

func (t* pebbleStorage) Get() *storage.GetOperation {
	return &storage.GetOperation{Storage: t}
}

func (t* pebbleStorage) Set() *storage.SetOperation {
	return &storage.SetOperation{Storage: t}
}

func (t* pebbleStorage) CompareAndSet() *storage.CompareAndSetOperation {
	return &storage.CompareAndSetOperation{Storage: t}
}

func (t *pebbleStorage) Increment() *storage.IncrementOperation {
	return &storage.IncrementOperation{Storage: t, Initial: 0, Delta: 1}
}

func (t* pebbleStorage) Remove() *storage.RemoveOperation {
	return &storage.RemoveOperation{Storage: t}
}

func (t* pebbleStorage) Enumerate() *storage.EnumerateOperation {
	return &storage.EnumerateOperation{Storage: t}
}

func (t* pebbleStorage) GetRaw(prefix, key []byte, ttlPtr *int, versionPtr *int64, required bool) ([]byte, error) {
	return t.getImpl(prefix, key, required)
}

func (t* pebbleStorage) SetRaw(prefix, key, value []byte, ttlSeconds int) error {
	return t.db.Set(append(prefix, key...), value, WriteOptions)
}

func (t *pebbleStorage) DoInTransaction(prefix, key []byte, cb func(entry *storage.RawEntry) bool) error {

	rawKey := append(prefix, key...)

	rawEntry := &storage.RawEntry {
		Key: rawKey,
		Ttl: storage.NoTTL,
		Version: 0,
	}

	value, closer, err := t.db.Get(append(prefix, key...))
	if err != nil {
		if err != pebble.ErrNotFound {
			return err
		}
	}
	defer closer.Close()

	rawEntry.Value = value

	if !cb(rawEntry) {
		return ErrCanceled
	}

	return t.db.Set(rawKey, rawEntry.Value, WriteOptions)
}

func (t* pebbleStorage) CompareAndSetRaw(bucket, key, value []byte, ttlSeconds int, version int64) (bool, error) {
	return true, t.SetRaw(bucket, key, value, ttlSeconds)
}

func (t* pebbleStorage) RemoveRaw(prefix, key []byte) error {
	return t.db.Delete(append(prefix, key...), WriteOptions)
}

func (t* pebbleStorage) getImpl(prefix, key []byte, required bool) ([]byte, error) {

	value, closer, err := t.db.Get(append(prefix, key...))
	if err != nil {
		if err == pebble.ErrNotFound {
			if required {
				return nil, storage.ErrNotFound
			}
		}
		return nil, err
	}

	dst := make([]byte, len(value))
	copy(dst, value)
	return dst, closer.Close()
}

func (t* pebbleStorage) EnumerateRaw(prefix, seek []byte, batchSize int, onlyKeys bool, cb func(entry *storage.RawEntry) bool) error {

	iter := t.db.NewIter(&pebble.IterOptions{
		LowerBound:  seek,
	})

	for iter.Valid() {

		if !bytes.HasPrefix(iter.Key(), prefix) {
			break
		}

		re := storage.RawEntry{
			Key:     iter.Key(),
			Value:   iter.Value(),
			Ttl:     0,
			Version: 0,
		}

		if !cb(&re) {
			break
		}

		if !iter.Next() {
			break
		}

	}

	return iter.Close()
}

func (t* pebbleStorage) FetchKeysRaw(prefix []byte, batchSize int) ([][]byte, error) {

	var list [][]byte

	iter := t.db.NewIter(&pebble.IterOptions{
		LowerBound:  prefix,
	})

	for iter.Valid() {

		if !bytes.HasPrefix(iter.Key(), prefix) {
			break
		}

		key := iter.Key()
		dst := make([]byte, len(key))
		copy(dst, key)
		list = append(list, dst)

		if !iter.Next() {
			break
		}

	}

	return list, iter.Close()
}

func (t* pebbleStorage) First() ([]byte, error) {
	iter := t.db.NewIter(&pebble.IterOptions{})
	defer iter.Close()
	if !iter.First() {
		return nil, nil
	}
	key := iter.Key()
	dst := make([]byte, len(key))
	copy(dst, key)
	return dst, nil
}

func (t* pebbleStorage) Last() ([]byte, error) {
	iter := t.db.NewIter(&pebble.IterOptions{})
	defer iter.Close()
	if !iter.Last() {
		return nil, nil
	}
	key := iter.Key()
	dst := make([]byte, len(key))
	copy(dst, key)
	return dst, nil
}

func (t* pebbleStorage) Compact(discardRatio float64) error {
	first, err := t.First()
	if err != nil {
		return err
	}
	last, err := t.Last()
	if err != nil {
		return err
	}
	return t.db.Compact(first, last)
}

func (t* pebbleStorage) Backup(w io.Writer, since uint64) (uint64, error) {
	snap := t.db.NewSnapshot()
	defer snap.Close()
	iter := snap.NewIter(&pebble.IterOptions{})

	packer := value.MessagePacker(w)
	for iter.Valid() {

		k, v := iter.Key(), iter.Value()
		if k != nil && v != nil {
			packer.PackBin(k)
			packer.PackBin(v)
		}

		if !iter.Next() {
			break
		}
	}

	return uint64(time.Now().Unix()), iter.Close()
}

func (t* pebbleStorage) Restore(r io.Reader) error {

	if err := t.DropAll(); err != nil {
		return err
	}

	unpacker := value.MessageReader(r)
	parser := value.MessageParser()

	readBinary := func() ([]byte, error) {
		fmt, header := unpacker.Next()
		if fmt == value.EOF {
			return nil, io.EOF
		}
		if fmt != value.BinHeader {
			return nil, ErrInvalidFormat
		}
		size := parser.ParseBin(header)
		if parser.Error() != nil {
			return nil, parser.Error()
		}
		return unpacker.Read(size)
	}

	for {

		key, err := readBinary()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		value, err := readBinary()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		err = t.db.Set(key, value, WriteOptions)
		if err != nil {
			return err
		}
	}

	return nil
}

func (t* pebbleStorage) DropAll() error {
	first, err := t.First()
	if err != nil {
		return err
	}
	last, err := t.Last()
	if err != nil {
		return err
	}
	return t.db.DeleteRange(first, append(last, 0xFF), WriteOptions)
}

func (t* pebbleStorage) DropWithPrefix(prefix []byte) error {

	last := append(prefix, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)

	return t.db.DeleteRange(prefix, last, WriteOptions)
}

func (t* pebbleStorage) Instance() interface{} {
	return t.db
}