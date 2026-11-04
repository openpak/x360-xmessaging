package xmessaging

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openpak/x360-common/wire"
)

const (
	alice = 0x0009000000000001
	bob   = 0x0009000000000002
	hdrZ  = "0000000000000000000000000000000000000000000000000000000000000000"
)

type ids struct{}

func (ids) Identity(_ context.Context, a string) (uint64, string, error) { return alice, "Friend", nil }
func (ids) AccountByXUID(_ context.Context, x uint64) (string, bool, error) {
	return "acct", x == alice || x == bob, nil
}

func newH(t *testing.T) *Handler {
	st := NewMemoryStore()
	// 2026-11-02 00:00 UTC = 752198400 s after 2003-01-01.
	return &Handler{Store: st, IDs: ids{}, Now: func() time.Time { return time.Unix(752198400+epoch2003, 0) }}
}

func req(typ uint32, f []byte) []byte {
	b := make([]byte, hdrLen, hdrLen+len(f))
	binary.LittleEndian.PutUint32(b, typ)
	binary.LittleEndian.PutUint32(b[4:], uint32(len(f)))
	return append(b, f...)
}

func u(vals ...uint64) []byte { // xuid then u32s
	b := binary.LittleEndian.AppendUint64(nil, vals[0])
	for _, v := range vals[1:] {
		b = binary.LittleEndian.AppendUint32(b, uint32(v))
	}
	return b
}

func call(t *testing.T, h *Handler, who uint64, body []byte) []byte {
	t.Helper()
	r := httptest.NewRequest("POST", Path, bytes.NewReader(body))
	r.Header.Set(wire.UsersHeader, hex16(who)+":acct")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	return w.Body.Bytes()
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		panic(err)
	}
	return b
}

func TestEmptyInbox(t *testing.T) {
	got := call(t, newH(t), alice, req(0x1B, u(alice)))
	if want := mustHex(hdrZ + "00000000 0000"); !bytes.Equal(got, want) {
		t.Fatalf("%x", got)
	}
}

func TestOneMessageListAndDetails(t *testing.T) {
	h := newH(t)
	props := mustHex("0100 0500 c4060000 0003486900")
	h.Store.Add(alice, Msg{Sender: 0x0009000000000001 << 0, Sent: 752198400, Flags: 0xFFFE07D1 - 0xFFFE07D1, Title: titleDash, Expire: 43200, Type: 8, Tag: "Friend", Subject: "Hi", Props: props}, 752198400)
	got := call(t, h, alice, req(0x1B, u(alice)))
	rec := got[hdrLen+6:]
	if len(got) != 130 || got[hdrLen+4] != 1 || len(rec) != 92 {
		t.Fatalf("list %x", got)
	}
	if !bytes.HasPrefix(rec[0x10:], mustHex("00a3d52c 01000000 00000000 d107feff c0a8 0d00 08 467269656e64")) {
		t.Fatalf("record %x", rec)
	}
	det := call(t, h, alice, req(0xF, u(alice, 1, 0, 0)))
	if want := append(append(mustHex(hdrZ+"00000000"), rec[:0x34]...), props...); !bytes.Equal(det, want) {
		t.Fatalf("details %x", det)
	}
}

func TestSendThenListDeleteFlags(t *testing.T) {
	h := newH(t)
	props := mustHex("0100 0500 c4060000 0003486900")
	f := binary.LittleEndian.AppendUint64(nil, alice)
	f = append(f, make([]byte, 8)...)              // context
	f = binary.LittleEndian.AppendUint32(f, 0)     // flags
	f = binary.LittleEndian.AppendUint32(f, 0)     // logon
	f = binary.LittleEndian.AppendUint16(f, 43200) // expire
	f = binary.LittleEndian.AppendUint16(f, uint16(len(props)))
	f = binary.LittleEndian.AppendUint16(f, 1) // n
	f = append(f, 8)
	f = append(f, make([]byte, 15)...)
	f = binary.LittleEndian.AppendUint64(f, bob)
	f = append(f, props...)
	got := call(t, h, alice, req(0x11, f))
	if want := mustHex(hdrZ + "00000000 0100 0200000000000900 00000000"); !bytes.Equal(got, want) {
		t.Fatalf("send reply %x", got)
	}
	if h.Store.Unread(bob, h.now()) != 1 {
		t.Fatal("unread")
	}
	list := call(t, h, bob, req(0x1B, u(bob)))
	if list[hdrLen+4] != 1 || string(list[hdrLen+6+0x25:hdrLen+6+0x2B]) != "Friend" {
		t.Fatalf("list %x", list)
	}
	// alice cannot read bob's inbox
	r := httptest.NewRequest("POST", Path, bytes.NewReader(req(0x1B, u(bob))))
	r.Header.Set(wire.UsersHeader, hex16(alice)+":acct")
	if w := httptest.NewRecorder(); func() int { h.ServeHTTP(w, r); return w.Code }() != 403 {
		t.Fatal("foreign inbox")
	}
	// set read flag, unread drops
	if got := call(t, h, bob, req(0x15, u(bob, 1, FlagRead, 0))); !bytes.Equal(got, mustHex(hdrZ+"00000000")) {
		t.Fatalf("flags %x", got)
	}
	if h.Store.Unread(bob, h.now()) != 0 {
		t.Fatal("still unread")
	}
	// delete with block flag, then it is gone
	if got := call(t, h, bob, req(0x10, u(bob, 1, 1))); !bytes.Equal(got, mustHex(hdrZ+"00000000")) {
		t.Fatalf("delete %x", got)
	}
	if got := call(t, h, bob, req(0x1B, u(bob))); len(got) != 38 {
		t.Fatalf("not empty %x", got)
	}
}

func TestLimitsAndExpiry(t *testing.T) {
	h := newH(t)
	for i := 0; i < 130; i++ {
		h.Store.Add(bob, Msg{Expire: 10, Sent: h.now()}, h.now())
	}
	if n := len(h.Store.List(bob, h.now())); n != 125 {
		t.Fatalf("cap %d", n)
	}
	if n := len(h.Store.List(bob, h.now()+601)); n != 0 {
		t.Fatalf("expiry %d", n)
	}
	if ok, _ := h.Store.Add(bob, Msg{Expire: 0}, h.now()); ok {
		t.Fatal("expire 0 stored")
	}
}

func TestInternalUnread(t *testing.T) {
	h := newH(t)
	h.Store.Add(bob, Msg{Expire: 10, Sent: h.now()}, h.now())
	ts := h.Internal("k")
	for key, want := range map[string]int{"": 403, "k": 200} {
		r := httptest.NewRequest("GET", "/internal/v1/unread?xuid="+hex16(bob), nil)
		r.Header.Set("X-Internal-Key", key)
		w := httptest.NewRecorder()
		ts.ServeHTTP(w, r)
		if w.Code != want || (want == 200 && w.Body.String() != `{"unread":1}`) {
			t.Fatalf("%q: %d %s", key, w.Code, w.Body)
		}
	}
	_ = http.StatusOK
}

func TestEraseDeletesInboxAndBlanksSent(t *testing.T) {
	st := NewMemoryStore()
	now := uint32(752198400)
	st.Add(alice, Msg{Sender: bob, Tag: "Bob", Sent: now, Expire: 60, Subject: "hi"}, now)
	st.Add(bob, Msg{Sender: alice, Tag: "Alice", Sent: now, Expire: 60, Subject: "yo"}, now)
	st.Add(bob, Msg{Sender: 0x0009000000000003, Tag: "Carol", Sent: now, Expire: 60}, now)
	n, err := st.Erase([]uint64{alice})
	if err != nil || n != 2 { // alice's inbox + one message she sent
		t.Fatalf("erase: %d %v", n, err)
	}
	if n, err := st.Erase([]uint64{alice}); err != nil || n != 0 {
		t.Fatalf("second erase: %d %v", n, err)
	}
	if len(st.List(alice, now)) != 0 {
		t.Fatal("inbox survived")
	}
	l := st.List(bob, now)
	if len(l) != 2 {
		t.Fatalf("bob's inbox: %+v", l)
	}
	for _, m := range l {
		if m.Subject == "yo" && (m.Sender != 0 || m.Tag != "") {
			t.Fatalf("sender kept: %+v", m)
		}
		if m.Tag == "Carol" && m.Sender == 0 {
			t.Fatal("other sender blanked")
		}
	}
}
