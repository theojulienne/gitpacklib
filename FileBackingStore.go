package gitpacklib

import (
	"encoding/hex"
	"errors"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/nightlyone/lockfile"
)

type FileBackingStore struct {
	basePath string
	lock     lockfile.Lockfile
	locked   bool
}

func NewFileBackingStore(basePath string) (*FileBackingStore, error) {
	absPath, err := filepath.Abs(basePath)
	if err != nil {
		return nil, err
	}

	os.MkdirAll(absPath, 0755)

	lock, err := lockfile.New(absPath + "/lock")
	if err != nil {
		return nil, err
	}

	return &FileBackingStore{absPath, lock, false}, nil
}

func (fs *FileBackingStore) keyPath(name string) string {
	safeFilename := hex.EncodeToString([]byte(name))
	bucket := safeFilename
	if len(bucket) > 20 {
		bucket = bucket[:20]
	}
	bucketPath := path.Join(fs.basePath, bucket)
	os.MkdirAll(bucketPath, 0755)
	return path.Join(bucketPath, safeFilename)
}

func (fs *FileBackingStore) Lock() {
	for fs.lock.TryLock() != nil {
		time.Sleep(100 * time.Millisecond)
	}
	fs.locked = true
}

func (fs *FileBackingStore) Unlock() {
	fs.lock.Unlock()
	fs.locked = false
}

func (fs *FileBackingStore) Set(name string, value []byte) (err error) {
	if !fs.locked {
		return errors.New("Lock must be aquired before calling Set")
	}
	path := fs.keyPath(name)
	err = ioutil.WriteFile(path, value, 0644)
	return err
}

func (fs *FileBackingStore) Get(name string) ([]byte, error) {
	if !fs.locked {
		return nil, errors.New("Lock must be aquired before calling Get")
	}
	path := fs.keyPath(name)
	data, err := ioutil.ReadFile(path)
	return data, err
}
