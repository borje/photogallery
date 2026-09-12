package api

import "testing"

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
