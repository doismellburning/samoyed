// SPDX-FileCopyrightText: The Samoyed Authors
// SPDX-License-Identifier: AGPL-3.0-or-later

package bbs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Store keeps messages on disk, a file each, with all of them in memory too:
// a packet BBS holds thousands of messages, not millions.  Each write goes to
// a temporary file renamed into place, so a crash leaves the old message or
// the new one, never half of one.
type Store struct {
	dir      string
	messages map[int]*Message
	bids     map[string]int
	next     int
}

// messageExt names a message's file, after its number.
const messageExt = ".json"

// OpenStore opens the store in dir, making it if need be.
func OpenStore(dir string) (*Store, error) {
	var err = os.MkdirAll(dir, 0o750)
	if err != nil {
		return nil, fmt.Errorf("bbs: %w", err)
	}

	var s = new(Store)
	s.dir = dir
	s.messages = make(map[int]*Message)
	s.bids = make(map[string]int)
	s.next = 1

	var entries, rerr = os.ReadDir(dir)
	if rerr != nil {
		return nil, fmt.Errorf("bbs: %w", rerr)
	}

	for _, e := range entries {
		var name = e.Name()
		if e.IsDir() || !strings.HasSuffix(name, messageExt) {
			continue
		}

		var n, nerr = strconv.Atoi(strings.TrimSuffix(name, messageExt))
		if nerr != nil {
			continue
		}

		var data, ferr = os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // G304: a name ReadDir found in the store's own directory.
		if ferr != nil {
			return nil, fmt.Errorf("bbs: %w", ferr)
		}

		var m = new(Message)

		var jerr = json.Unmarshal(data, m)
		if jerr != nil || m.Number != n {
			return nil, fmt.Errorf("bbs: message file %s is damaged", name)
		}

		s.messages[n] = m
		s.bids[strings.ToUpper(m.BID)] = n
		s.next = max(s.next, n+1)
	}

	return s, nil
}

// HasBID says whether a message with this BID (or MID) is already here.
func (s *Store) HasBID(bid string) bool {
	var _, ok = s.bids[strings.ToUpper(bid)]

	return ok
}

// bidLen is the longest BID this BBS makes, as FBB and Winlink both limit them.
const bidLen = 12

// maxBIDLen is the longest BID taken from a partner, more lenient than bidLen
// with what others make.
const maxBIDLen = 40

// ErrDuplicate is returned for a message whose BID is already here.
var ErrDuplicate = errors.New("bbs: already have a message with that BID")

// Add stores m as a new message, giving it the next number and, if it has
// none, a BID made from that number and bbsCall.
func (s *Store) Add(m *Message, bbsCall string) error {
	if m.BID == "" {
		m.BID = fmt.Sprintf("%d_%s", s.next, baseCall(bbsCall))
		if len(m.BID) > bidLen {
			m.BID = m.BID[:bidLen]
		}
	}

	if s.HasBID(m.BID) {
		return ErrDuplicate
	}

	m.Number = s.next

	var err = s.write(m)
	if err != nil {
		return err
	}

	s.next++
	s.messages[m.Number] = m
	s.bids[strings.ToUpper(m.BID)] = m.Number

	return nil
}

// Update writes a changed message back.
func (s *Store) Update(m *Message) error {
	if _, ok := s.messages[m.Number]; !ok {
		return fmt.Errorf("bbs: no message %d", m.Number)
	}

	return s.write(m)
}

// Get returns message n.
func (s *Store) Get(n int) (*Message, bool) {
	var m, ok = s.messages[n]

	return m, ok
}

// Delete removes message n.  Its BID stays known for as long as the store is
// open, so it is not taken again from a partner that offers it.
func (s *Store) Delete(n int) error {
	if _, ok := s.messages[n]; !ok {
		return fmt.Errorf("bbs: no message %d", n)
	}

	var err = os.Remove(s.path(n))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("bbs: %w", err)
	}

	delete(s.messages, n)

	return nil
}

// All returns every message, oldest first.
func (s *Store) All() []*Message {
	var list = make([]*Message, 0, len(s.messages))
	for _, m := range s.messages {
		list = append(list, m)
	}

	slices.SortFunc(list, func(a, b *Message) int { return a.Number - b.Number })

	return list
}

func (s *Store) path(n int) string {
	return filepath.Join(s.dir, fmt.Sprintf("%06d%s", n, messageExt))
}

func (s *Store) write(m *Message) error {
	data, err := json.MarshalIndent(m, "", "  ") // := for errchkjson, which doesn't follow var.
	if err != nil {
		return fmt.Errorf("bbs: %w", err)
	}

	var tmp, terr = os.CreateTemp(s.dir, ".tmp-*")
	if terr != nil {
		return fmt.Errorf("bbs: %w", terr)
	}

	var _, werr = tmp.Write(data)
	var serr = tmp.Sync()
	var cerr = tmp.Close()

	var failed = errors.Join(werr, serr, cerr)
	if failed == nil {
		failed = os.Rename(tmp.Name(), s.path(m.Number))
	}

	if failed != nil {
		_ = os.Remove(tmp.Name())

		return fmt.Errorf("bbs: %w", failed)
	}

	return nil
}
