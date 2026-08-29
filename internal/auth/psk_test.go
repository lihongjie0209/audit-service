package auth

import "testing"

func TestVerifyPSK(t *testing.T) {
	t.Parallel()
	const key = "01234567890123456789012345678901"
	for _, test := range []struct {
		name   string
		header string
		want   bool
	}{
		{name: "valid", header: "PSK " + key, want: true},
		{name: "case insensitive scheme", header: "psk " + key, want: true},
		{name: "wrong key", header: "PSK 11234567890123456789012345678901"},
		{name: "wrong scheme", header: "Bearer " + key},
		{name: "missing", header: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := VerifyPSK(test.header, key); got != test.want {
				t.Fatalf("VerifyPSK() = %v, want %v", got, test.want)
			}
		})
	}
}
