package obfuscate

import "testing"

func TestRoundTrip(t *testing.T) {
	key := []byte{0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x6f, 0x70, 0x81,
		0x92, 0xa3, 0xb4, 0xc5, 0xd6, 0xe7, 0xf8, 0x09}
	cases := []string{
		"sk-f6e9280c4c91dbc8",
		"https://adb-123456.azuredatabricks.net",
		"",
		"short",
	}
	for _, tc := range cases {
		enc := Encode(tc, key)
		got := Decode(enc)
		if got != tc {
			t.Errorf("Encode/Decode(%q) = %q, want %q", tc, got, tc)
		}
	}
}

func TestDecodePassthrough(t *testing.T) {
	if Decode("not-encoded") != "not-encoded" {
		t.Error("non-encoded string should pass through")
	}
}
