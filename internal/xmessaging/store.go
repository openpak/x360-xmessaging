package xmessaging

import (
	"encoding/json"
	"os"
	"sync"
)

const maxInbox = 125 // native inbox cap, oldest dropped

// Msg is one stored message (the 92-byte record of docs/protocol.md, minus the wire packing).
type Msg struct {
	Sender  uint64 `json:"sender"`
	Context uint64 `json:"context"`
	Sent    uint32 `json:"sent"` // seconds since 2003-01-01 UTC
	ID      uint32 `json:"id"`
	Flags   uint32 `json:"flags"`
	Title   uint32 `json:"title"`
	Expire  uint16 `json:"expire"` // minutes from Sent
	Type    uint8  `json:"type"`
	Tag     string `json:"tag"`
	Subject string `json:"subject"`
	Props   []byte `json:"props"`
}

type inbox struct {
	Next uint32 `json:"next"`
	Msgs []Msg  `json:"msgs"` // newest first
}

// Store keeps every user's inbox in one JSON file, saved atomically on each change.
type Store struct {
	mu    sync.Mutex
	path  string
	Boxes map[string]*inbox `json:"boxes"` // key: 16 hex digits of the XUID
}

// Open loads path (empty: memory only).
func Open(path string) (*Store, error) {
	s := &Store{path: path, Boxes: map[string]*inbox{}}
	if path == "" {
		return s, nil
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, err
	}
	if s.Boxes == nil {
		s.Boxes = map[string]*inbox{}
	}
	return s, nil
}

func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := s.path + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func key(xuid uint64) string { return hex16(xuid) }

func expired(m *Msg, now uint32) bool {
	return m.Expire == 0 || uint64(m.Sent)+uint64(m.Expire)*60 <= uint64(now)
}

// box returns the inbox with expired messages purged (caller holds mu).
func (s *Store) box(xuid uint64, now uint32) *inbox {
	b := s.Boxes[key(xuid)]
	if b == nil {
		return nil
	}
	keep := b.Msgs[:0]
	for _, m := range b.Msgs {
		if !expired(&m, now) {
			keep = append(keep, m)
		}
	}
	b.Msgs = keep
	return b
}

// Add stores m for xuid (ID assigned) and reports false when it is already expired.
func (s *Store) Add(xuid uint64, m Msg, now uint32) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if expired(&m, now) {
		return false, nil
	}
	b := s.box(xuid, now)
	if b == nil {
		b = &inbox{}
		s.Boxes[key(xuid)] = b
	}
	b.Next++
	m.ID = b.Next
	b.Msgs = append([]Msg{m}, b.Msgs...)
	if len(b.Msgs) > maxInbox {
		b.Msgs = b.Msgs[:maxInbox]
	}
	return true, s.saveLocked()
}

// List returns a copy of the live messages, newest first.
func (s *Store) List(xuid uint64, now uint32) []Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b := s.box(xuid, now); b != nil {
		return append([]Msg(nil), b.Msgs...)
	}
	return nil
}

// Update applies f to message id and saves; false when it does not exist.
func (s *Store) Update(xuid uint64, id uint32, now uint32, f func(*Msg)) (Msg, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b := s.box(xuid, now); b != nil {
		for i := range b.Msgs {
			if b.Msgs[i].ID == id {
				f(&b.Msgs[i])
				return b.Msgs[i], true, s.saveLocked()
			}
		}
	}
	return Msg{}, false, nil
}

// Delete removes message id; false when it does not exist.
func (s *Store) Delete(xuid uint64, id uint32, now uint32) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b := s.box(xuid, now); b != nil {
		for i := range b.Msgs {
			if b.Msgs[i].ID == id {
				b.Msgs = append(b.Msgs[:i], b.Msgs[i+1:]...)
				return true, s.saveLocked()
			}
		}
	}
	return false, nil
}

// Unread counts live messages without the read flag.
func (s *Store) Unread(xuid uint64, now uint32) int {
	n := 0
	for _, m := range s.List(xuid, now) {
		if m.Flags&FlagRead == 0 {
			n++
		}
	}
	return n
}
