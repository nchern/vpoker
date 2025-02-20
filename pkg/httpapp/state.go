package httpapp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sync"
)

const statePath = "/tmp/vpoker.json"

type stateFile struct {
	path string
	lock sync.RWMutex
}

func NewStateFile(path string) *stateFile {
	return &stateFile{path: path}
}

func (s *stateFile) save(marshalers ...json.Marshaler) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	f, err := os.Create(statePath)
	defer func() { logError(f.Close(), "stateFile.save os.Create") }()
	if err != nil {
		return err
	}
	for _, v := range marshalers {
		b, err := v.MarshalJSON()
		if err != nil {
			return err
		}
		if _, err := f.Write(b); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(f); err != nil {
			return err
		}
	}
	return err
}

func (s *stateFile) load(unmarshalers ...json.Unmarshaler) error {
	s.lock.RLock()
	defer s.lock.RUnlock()
	if err := os.MkdirAll(path.Dir(statePath), 0700); err != nil {
		return err
	}
	f, err := os.Open(statePath)
	defer func() { logError(f.Close(), "stateFile.load os.Open") }()
	if err != nil {
		return err
	}
	r := bufio.NewReader(f)
	for _, v := range unmarshalers {
		l, err := r.ReadString('\n')
		if err != nil {
			return err
		}
		if err := v.UnmarshalJSON([]byte(l)); err != nil {
			return err
		}
	}
	return nil
}
