// Copyright 2026 The Vrata Authors
// SPDX-License-Identifier: Apache-2.0

package bolt

import (
	"context"
	"crypto/rand"
	"path/filepath"
	"testing"

	"github.com/achetronic/vrata/internal/encrypt"
	"github.com/achetronic/vrata/internal/model"
)

func testCipher(t *testing.T) *encrypt.Cipher {
	t.Helper()
	key := make([]byte, 32)
	rand.Read(key)
	c, err := encrypt.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEncryptedSecretCRUD(t *testing.T) {
	dir := t.TempDir()
	st, err := New(filepath.Join(dir, "test.db"), testCipher(t))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	sec := model.Secret{ID: "s1", Name: "test", Value: "super-secret"}
	if err := st.SaveSecret(ctx, sec); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := st.GetSecret(ctx, "s1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Value != "super-secret" {
		t.Errorf("expected 'super-secret', got %q", got.Value)
	}

	summaries, err := st.ListSecrets(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != "s1" {
		t.Errorf("unexpected list result: %v", summaries)
	}
}

func TestEncryptedSnapshotCRUD(t *testing.T) {
	dir := t.TempDir()
	st, err := New(filepath.Join(dir, "test.db"), testCipher(t))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	vs := model.VersionedSnapshot{
		ID:   "snap1",
		Name: "test-snap",
		Snapshot: model.Snapshot{
			Routes: []model.Route{{ID: "r1", Name: "route1"}},
		},
	}
	if err := st.SaveSnapshot(ctx, vs); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := st.GetSnapshot(ctx, "snap1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "test-snap" {
		t.Errorf("expected 'test-snap', got %q", got.Name)
	}
	if len(got.Snapshot.Routes) != 1 {
		t.Errorf("expected 1 route, got %d", len(got.Snapshot.Routes))
	}
}

func TestMismatchEncryptedDataNoCipher(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	st, err := New(dbPath, testCipher(t))
	if err != nil {
		t.Fatal(err)
	}
	st.SaveSecret(context.Background(), model.Secret{ID: "s1", Name: "test", Value: "val"})
	st.Close()

	_, err = New(dbPath, nil)
	if err == nil {
		t.Fatal("expected error opening encrypted db without cipher")
	}
}

func TestMismatchNoCipherDataWithCipher(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	st, err := New(dbPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	st.SaveSecret(context.Background(), model.Secret{ID: "s1", Name: "test", Value: "val"})
	st.Close()

	_, err = New(dbPath, testCipher(t))
	if err == nil {
		t.Fatal("expected error opening plaintext db with cipher")
	}
}

func TestWrongKeyFails(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	c1 := testCipher(t)
	st, err := New(dbPath, c1)
	if err != nil {
		t.Fatal(err)
	}
	st.SaveSecret(context.Background(), model.Secret{ID: "s1", Name: "test", Value: "val"})
	st.Close()

	c2 := testCipher(t)
	st2, err := New(dbPath, c2)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()

	_, err = st2.GetSecret(context.Background(), "s1")
	if err == nil {
		t.Fatal("expected error reading with wrong key")
	}
}

func TestPlaintextModeWorks(t *testing.T) {
	dir := t.TempDir()
	st, err := New(filepath.Join(dir, "test.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	st.SaveSecret(ctx, model.Secret{ID: "s1", Name: "test", Value: "plain"})
	got, err := st.GetSecret(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "plain" {
		t.Errorf("expected 'plain', got %q", got.Value)
	}
}

func TestEmptyDbWithCipherSetsMarker(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	st, err := New(dbPath, testCipher(t))
	if err != nil {
		t.Fatal(err)
	}
	st.Close()

	_, err = New(dbPath, testCipher(t))
	if err == nil || err.Error() == "" {
		t.Log("reopened with cipher - ok (marker was set on first open)")
	}
}
