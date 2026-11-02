// Package xmessaging serves the Xbox Live message inbox, Live service 18
// (/xpnfront/xmessaging.srf): list, fetch, details, flags, delete and send
// (docs/protocol.md). The wire layouts come from the XAM 17559 RE note.
package xmessaging

import (
	"context"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/openpak/x360-common/wire"
)

const (
	Path        = "/xpnfront/xmessaging.srf"
	ContentType = "xon/5"
	maxBody     = 64 << 10

	// FlagRead is the "read" bit; the dash module that sets it is absent, so the value is
	// inferred (flags set through 0x504 live in the top byte).
	FlagRead = 0x01000000

	hdrLen        = 32
	recLen        = 92
	maxRecipients = 100
	maxExpire     = 0xA8C0 // 30 days in minutes
	maxProps      = 0x3FF * 4
	titleDash     = 0xFFFE07D1

	hrNoMessage = 0x80155A04 // inferred
	hrNoDetails = 0x80155A05
	hrFail      = 0x80004005
)

// epoch2003 is the message clock origin (unix seconds of 2003-01-01 UTC).
const epoch2003 = 1041379200

// IDs is the identity data x360-xuacs owns.
type IDs interface {
	Identity(ctx context.Context, account string) (uint64, string, error)
	AccountByXUID(ctx context.Context, xuid uint64) (string, bool, error)
}

// Handler serves Path. Wrap it with wire.Guard.
type Handler struct {
	Store *Store
	IDs   IDs
	Log   *slog.Logger
	Now   func() time.Time
}

func (h *Handler) log() *slog.Logger {
	if h.Log != nil {
		return h.Log
	}
	return slog.Default()
}

func (h *Handler) now() uint32 {
	t := time.Now()
	if h.Now != nil {
		t = h.Now()
	}
	return uint32(t.Unix() - epoch2003)
}

type reqErr struct {
	status int
	msg    string
}

func (e *reqErr) Error() string           { return e.msg }
func deny(status int, msg string) *reqErr { return &reqErr{status, msg} }

func hex16(x uint64) string { return fmt.Sprintf("%016x", x) }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		return
	}
	if !strings.EqualFold(r.URL.Path, Path) {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	var out []byte
	if err == nil {
		out, err = h.serve(r.Context(), body, r.Header.Get(wire.UsersHeader))
	}
	if err != nil {
		status := http.StatusServiceUnavailable
		var re *reqErr
		if errors.As(err, &re) {
			status = re.status
		}
		h.log().Warn("xmessaging refused", "status", status, "body_len", len(body), "err", err)
		http.Error(w, http.StatusText(status), status)
		return
	}
	w.Header().Set("Content-Type", ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(out)))
	w.Write(out)
}

// ticketUsers parses the gateway's users header strictly (same rules as x360-xpnfront).
func ticketUsers(header string) (map[uint64]string, error) {
	users := make(map[uint64]string)
	for _, entry := range strings.Split(header, ",") {
		x, account, ok := strings.Cut(entry, ":")
		if !ok || len(x) != 16 || account == "" {
			return nil, deny(403, "missing tunnel user")
		}
		xuid, err := strconv.ParseUint(x, 16, 64)
		if err != nil || xuid>>48 != 9 || users[xuid] != "" {
			return nil, deny(403, "invalid tunnel user")
		}
		users[xuid] = account
	}
	if len(users) > 4 {
		return nil, deny(403, "too many tunnel users")
	}
	return users, nil
}

func hr(code uint32) []byte {
	out := make([]byte, hdrLen+4)
	binary.LittleEndian.PutUint32(out[hdrLen:], code)
	return out
}

func (h *Handler) serve(ctx context.Context, body []byte, usersHeader string) ([]byte, error) {
	users, err := ticketUsers(usersHeader)
	if err != nil {
		return nil, err
	}
	if len(body) < hdrLen || len(body) != hdrLen+int(binary.LittleEndian.Uint32(body[4:])) {
		return nil, deny(400, "invalid message request")
	}
	typ, f := binary.LittleEndian.Uint32(body), body[hdrLen:]
	if len(f) < 8 || users[binary.LittleEndian.Uint64(f)] == "" {
		return nil, deny(403, "xuid is not on the ticket")
	}
	le := binary.LittleEndian
	now := h.now()
	switch {
	case typ == 0x1B && len(f) == 8: // 0x513 list
		msgs := h.Store.List(le.Uint64(f), now)
		out := hr(0)
		out = le.AppendUint16(out, uint16(len(msgs)))
		for i := range msgs {
			out = append(out, record(&msgs[i])...)
		}
		return out, nil
	case typ == 0x1C && len(f) == 12: // 0x514 fetch one
		for _, m := range h.Store.List(le.Uint64(f), now) {
			if m.ID == le.Uint32(f[8:]) {
				return append(hr(0), record(&m)...), nil
			}
		}
		return hr(hrNoMessage), nil
	case (typ == 0xF || typ == 0x15) && len(f) == 20: // 0x503 details, 0x504 flags
		set, clr := le.Uint32(f[12:]), le.Uint32(f[16:])
		m, ok, err := h.Store.Update(le.Uint64(f), le.Uint32(f[8:]), now, func(m *Msg) { m.Flags = (m.Flags | set) &^ clr })
		if err != nil {
			return nil, err
		}
		if !ok {
			return hr(hrNoMessage), nil
		}
		if typ == 0x15 {
			return hr(0), nil
		}
		if len(m.Props) == 0 {
			return hr(hrNoDetails), nil
		}
		return append(append(hr(0), record(&m)[:0x34]...), m.Props...), nil
	case typ == 0x10 && len(f) == 16: // 0x505 delete
		ok, err := h.Store.Delete(le.Uint64(f), le.Uint32(f[8:]), now)
		if err != nil {
			return nil, err
		}
		if !ok {
			return hr(hrNoMessage), nil
		}
		if le.Uint32(f[12:]) != 0 {
			// ponytail: x360-common/accountcore has no write block API; blocking the sender
			// from a deleted message is only logged until it does.
			h.log().Info("block sender requested", "xuid", hex16(le.Uint64(f)), "id", le.Uint32(f[8:]))
		}
		return hr(0), nil
	case typ == 0x11 && len(f) >= 0x2E: // 0x50A send
		if users[le.Uint64(f)] == "" {
			return nil, deny(403, "sender is not on the ticket")
		}
		return h.send(ctx, f, users[le.Uint64(f)], now)
	}
	// ponytail: 0x50B (revoke sent messages) has no known msg_type or fields; unsupported until probed.
	return nil, deny(400, "unsupported message request")
}

func (h *Handler) send(ctx context.Context, f []byte, account string, now uint32) ([]byte, error) {
	le := binary.LittleEndian
	psize, n, expire := int(le.Uint16(f[0x1A:])), int(le.Uint16(f[0x1C:])), le.Uint16(f[0x18:])
	if n < 1 || n > maxRecipients || expire > maxExpire || psize < 4 || psize > maxProps || len(f) != 0x2E+8*n+psize {
		return nil, deny(400, "invalid send")
	}
	props := f[0x2E+8*n:]
	np, nb := int(le.Uint16(props)), int(le.Uint16(props[2:]))
	if psize != 4+4*np+nb {
		return nil, deny(400, "invalid send properties")
	}
	_, tag, err := h.IDs.Identity(ctx, account)
	if err != nil {
		return nil, deny(503, "account service unavailable")
	}
	m := Msg{Sender: le.Uint64(f), Context: le.Uint64(f[8:]), Sent: now, Flags: le.Uint32(f[0x10:]) &^ FlagRead, Title: titleDash,
		Expire: expire, Type: f[0x1E], Tag: tag, Subject: subject(props, np), Props: append([]byte(nil), props...)}
	out := hr(0)
	out = le.AppendUint16(out, uint16(n))
	for i := 0; i < n; i++ {
		to := le.Uint64(f[0x2E+8*i:])
		code := uint32(0)
		if _, ok, err := h.IDs.AccountByXUID(ctx, to); err != nil {
			return nil, deny(503, "account service unavailable")
		} else if !ok {
			code = hrFail
		} else if _, err := h.Store.Add(to, m, now); err != nil {
			return nil, err
		}
		out = le.AppendUint64(out, to)
		out = le.AppendUint32(out, code)
	}
	h.log().Info("message sent", "from", hex16(m.Sender), "recipients", n, "type", m.Type)
	return out, nil
}

// subject is the text property 0x06C4 (string: blob u16 BE length + UTF-8 with NUL), "" when absent.
func subject(props []byte, np int) string {
	blob := props[4+4*np:]
	for i := 0; i < np; i++ {
		e := props[4+4*i:]
		if binary.LittleEndian.Uint16(e) != 0x06C4 {
			continue
		}
		off := int(binary.LittleEndian.Uint16(e[2:]))
		if off+2 > len(blob) {
			return ""
		}
		l := int(binary.BigEndian.Uint16(blob[off:]))
		if off+2+l > len(blob) {
			return ""
		}
		return strings.TrimRight(string(blob[off+2:off+2+l]), "\x00")
	}
	return ""
}

// record packs m into the 92-byte wire record.
func record(m *Msg) []byte {
	b := make([]byte, recLen)
	le := binary.LittleEndian
	le.PutUint64(b, m.Sender)
	le.PutUint64(b[8:], m.Context)
	le.PutUint32(b[0x10:], m.Sent)
	le.PutUint32(b[0x14:], m.ID)
	le.PutUint32(b[0x18:], m.Flags)
	le.PutUint32(b[0x1C:], m.Title)
	le.PutUint16(b[0x20:], m.Expire)
	le.PutUint16(b[0x22:], uint16(len(m.Props)))
	b[0x24] = m.Type
	copy(b[0x25:0x34], m.Tag)
	for i, u := range utf16.Encode([]rune(m.Subject)) {
		if i == 19 {
			break
		}
		le.PutUint16(b[0x34+2*i:], u)
	}
	return b
}

// Internal serves GET /internal/v1/unread?xuid=<16 hex> -> {"unread":N} for x360-xpnfront.
func (h *Handler) Internal(key string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Internal-Key")), []byte(key)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		x := r.URL.Query().Get("xuid")
		xuid, err := strconv.ParseUint(x, 16, 64)
		if r.URL.Path != "/internal/v1/unread" || r.Method != http.MethodGet || len(x) != 16 || err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, `{"unread":%d}`, h.Store.Unread(xuid, h.now()))
	})
}
