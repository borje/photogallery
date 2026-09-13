package api

import (
	"context"
	"errors"
	"testing"
)

// probeUpload queues on deriveSem; a client that has given up must not be
// left waiting for a token, and the token must not leak when it gives up.
func TestProbeUploadGivesUpWithTheClient(t *testing.T) {
	e := newEnvNoWorker(t)
	s := e.srv
	for range cap(s.deriveSem) {
		s.deriveSem <- struct{}{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.probeUpload(ctx, "src.jpg", "dst.jpg"); !errors.Is(err, context.Canceled) {
		t.Fatalf("probeUpload on a cancelled request = %v, want context.Canceled", err)
	}
	if len(s.deriveSem) != cap(s.deriveSem) {
		t.Fatalf("deriveSem holds %d tokens, want %d", len(s.deriveSem), cap(s.deriveSem))
	}
}

func TestHeadCaptureAcrossShortWrites(t *testing.T) {
	cases := []struct {
		name   string
		writes [][]byte
		want   [3]byte
	}{
		{"single write", [][]byte{{0xFF, 0xD8, 0xFF, 0xE0}}, [3]byte{0xFF, 0xD8, 0xFF}},
		{"two writes", [][]byte{{0xFF, 0xD8}, {0xFF, 0xE0, 0x00, 0x10}}, [3]byte{0xFF, 0xD8, 0xFF}},
		{"byte at a time", [][]byte{{0xFF}, {0xD8}, {0xFF}, {0xE0}}, [3]byte{0xFF, 0xD8, 0xFF}},
		{"empty then full", [][]byte{{}, {0xFF, 0xD8, 0xFF}}, [3]byte{0xFF, 0xD8, 0xFF}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up := &upload{}
			for _, w := range tc.writes {
				n, err := headCapture{up}.Write(w)
				if err != nil || n != len(w) {
					t.Fatalf("Write(%x) = %d, %v", w, n, err)
				}
			}
			if up.headLen != 3 || up.head != tc.want {
				t.Fatalf("head = %x (len %d), want %x", up.head, up.headLen, tc.want)
			}
		})
	}
}
