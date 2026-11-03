package ca

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"filippo.io/age"
)

type keyWriteFault struct {
	bytes.Buffer
	writeErr error
	closeErr error
	closes   int
}

func (f *keyWriteFault) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.Buffer.Write(p)
}

func (f *keyWriteFault) Close() error {
	f.closes++
	return f.closeErr
}

func TestWriteEncryptedKeyCompletion(t *testing.T) {
	writeErr := errors.New("injected write failure")
	closeErr := errors.New("injected close failure")
	for _, tc := range []struct {
		name     string
		writeErr error
		closeErr error
	}{
		{name: "success"},
		{name: "close failure", closeErr: closeErr},
		{name: "write failure", writeErr: writeErr},
		{name: "write and close failure", writeErr: writeErr, closeErr: closeErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, recipient := newX25519Pair(t)
			f := &keyWriteFault{writeErr: tc.writeErr, closeErr: tc.closeErr}
			plain := []byte("synthetic key bytes")
			err := writeEncryptedKey(f, plain, []age.Recipient{recipient})
			if f.closes != 1 {
				t.Fatalf("file closed %d times, want 1", f.closes)
			}
			for _, want := range []error{tc.writeErr, tc.closeErr} {
				if want != nil && !errors.Is(err, want) {
					t.Fatalf("error %v does not preserve %v", err, want)
				}
			}
			if tc.writeErr != nil || tc.closeErr != nil {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			r, err := age.Decrypt(bytes.NewReader(f.Bytes()), id)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, plain) {
				t.Fatal("decrypted key bytes differ")
			}
		})
	}
}
