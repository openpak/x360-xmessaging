package xmessaging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/openpak/x360-common/pgstate"
)

func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("X360_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("X360_TEST_DATABASE_URL not set")
	}
	return dsn
}

func pgSchema(t *testing.T) string {
	dsn := testDSN(t)
	b := make([]byte, 6)
	rand.Read(b)
	schema := "t_xmsg_" + hex.EncodeToString(b)
	t.Cleanup(func() {
		if p, err := pgstate.Open(context.Background(), dsn, schema); err == nil {
			p.Pool().Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
			p.Close()
		}
	})
	return schema
}

func TestOpenRefusesWithoutDSN(t *testing.T) {
	if _, err := Open(context.Background(), "", Schema, ""); err == nil {
		t.Fatal("started without a database URL")
	}
}

func TestInboxSurvivesRestart(t *testing.T) {
	schema := pgSchema(t)
	ctx := context.Background()
	s, err := Open(ctx, testDSN(t), schema, "")
	if err != nil {
		t.Fatal(err)
	}
	const x, now = 0x0009000000000001, 1000
	if ok, err := s.Add(x, Msg{Sent: now, Expire: 600, Subject: "hi"}, now); !ok || err != nil {
		t.Fatal(ok, err)
	}
	s.Close()
	s, err = Open(ctx, testDSN(t), schema, "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	l := s.List(x, now)
	if len(l) != 1 || l[0].Subject != "hi" || l[0].ID != 1 {
		t.Fatalf("%+v", l)
	}
}

func TestLegacyFileImported(t *testing.T) {
	file := filepath.Join(t.TempDir(), "inbox.json")
	js := `{"boxes":{"0009000000000001":{"next":3,"msgs":[{"sender":1,"sent":1000,"id":3,"expire":600,"subject":"old"}]}}}`
	if err := os.WriteFile(file, []byte(js), 0o600); err != nil {
		t.Fatal(err)
	}
	schema := pgSchema(t)
	s, err := Open(context.Background(), testDSN(t), schema, file)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	os.Remove(file)
	s, err = Open(context.Background(), testDSN(t), schema, file)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if l := s.List(0x0009000000000001, 1000); len(l) != 1 || l[0].Subject != "old" {
		t.Fatalf("%+v", l)
	}
}
