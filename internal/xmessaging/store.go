package xmessaging

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/openpak/x360-common/pgstate"
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

// Schema is the Postgres schema x360-xmessaging owns inside the x360 database.
const Schema = "x360_xmessaging"

// Store keeps every user's inbox in memory and writes the changed inbox as one
// document (key "box/<xuid>") to Postgres on each change; a nil pg is memory
// only (tests).
type Store struct {
	mu    sync.Mutex
	pg    *pgstate.Store
	Boxes map[string]*inbox `json:"boxes"` // key: 16 hex digits of the XUID
}

// NewMemoryStore is a store that forgets on exit (tests).
func NewMemoryStore() *Store { return &Store{Boxes: map[string]*inbox{}} }

// Open opens the Postgres store in schema and loads every inbox. When the
// database holds no inbox yet and legacyFile (the old inbox.json) exists, it is
// imported once and left in place. An empty dsn is an error.
func Open(ctx context.Context, dsn, schema, legacyFile string) (*Store, error) {
	pg, err := pgstate.Open(ctx, dsn, schema)
	if err != nil {
		return nil, err
	}
	s := NewMemoryStore()
	s.pg = pg
	rows, err := pg.Pool().Query(ctx, `SELECT key, value FROM docs WHERE key LIKE 'box/%'`)
	if err != nil {
		pg.Close()
		return nil, err
	}
	for rows.Next() {
		var k string
		var v []byte
		var b inbox
		if err = rows.Scan(&k, &v); err == nil {
			err = json.Unmarshal(v, &b)
		}
		if err != nil {
			break
		}
		s.Boxes[strings.TrimPrefix(k, "box/")] = &b
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err == nil && len(s.Boxes) == 0 && legacyFile != "" {
		b, rerr := os.ReadFile(legacyFile)
		switch {
		case rerr == nil:
			if err = json.Unmarshal(b, s); err == nil {
				for k := range s.Boxes {
					if err = s.saveBoxLocked(k); err != nil {
						break
					}
				}
			}
			if err == nil {
				slog.Info("imported inbox.json into Postgres", "inboxes", len(s.Boxes))
			}
		case !os.IsNotExist(rerr):
			err = rerr
		}
	}
	if err != nil {
		pg.Close()
		return nil, err
	}
	if s.Boxes == nil {
		s.Boxes = map[string]*inbox{}
	}
	return s, nil
}

// Close releases the database pool.
func (s *Store) Close() {
	if s.pg != nil {
		s.pg.Close()
	}
}

func (s *Store) saveBoxLocked(k string) error {
	if s.pg == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.pg.Save(ctx, "box/"+k, s.Boxes[k])
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
	return true, s.saveBoxLocked(key(xuid))
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
				return b.Msgs[i], true, s.saveBoxLocked(key(xuid))
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
				return true, s.saveBoxLocked(key(xuid))
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
