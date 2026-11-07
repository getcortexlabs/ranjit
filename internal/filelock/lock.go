// Package filelock coordinates Ranjit processes during offline maintenance.
package filelock

import (
	"errors"
	"os"
)

type Lock struct{ file *os.File }

func Acquire(path string, exclusive bool) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lock(f, exclusive); err != nil {
		f.Close()
		return nil, errors.New("database is in use; stop Ranjit before restoring or updating")
	}
	return &Lock{file: f}, nil
}
func (l *Lock) Close() error { return l.file.Close() }
