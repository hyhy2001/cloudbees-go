// Package obfuscate provides XOR encoding to prevent plaintext secrets
// from appearing in `strings ./bee` output.
// Format: hex(key) + ":" + hex(xor(plaintext, repeating key))
package obfuscate

import "encoding/hex"

// Encode encodes plaintext with the given key bytes.
func Encode(plaintext string, key []byte) string {
	pt := []byte(plaintext)
	ct := make([]byte, len(pt))
	for i := range pt {
		ct[i] = pt[i] ^ key[i%len(key)]
	}
	return hex.EncodeToString(key) + ":" + hex.EncodeToString(ct)
}

// Decode decodes a "hexKey:hexCiphertext" string.
// Returns the input unchanged if it doesn't look encoded.
func Decode(encoded string) string {
	for i, c := range encoded {
		if c == ':' {
			keyHex, ctHex := encoded[:i], encoded[i+1:]
			key, err1 := hex.DecodeString(keyHex)
			ct, err2 := hex.DecodeString(ctHex)
			if err1 != nil || err2 != nil || len(key) == 0 {
				return encoded
			}
			pt := make([]byte, len(ct))
			for j := range ct {
				pt[j] = ct[j] ^ key[j%len(key)]
			}
			return string(pt)
		}
	}
	return encoded
}
