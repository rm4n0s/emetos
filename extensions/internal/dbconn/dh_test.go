package dbconn

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"math/big"
	"testing"
)

func TestThePrimeIsTheSecondOakleyGroup(t *testing.T) {
	p := dhPrime()
	if p.BitLen() != 1024 {
		t.Fatalf("the prime has %d bits, want 1024", p.BitLen())
	}
	if !p.ProbablyPrime(30) {
		t.Fatal("the prime is not prime: a digit of it is wrong")
	}
	// (p-1)/2 is prime too: a safe prime, as the group is defined
	q := new(big.Int).Rsh(new(big.Int).Sub(p, big.NewInt(1)), 1)
	if !q.ProbablyPrime(30) {
		t.Fatal("(p-1)/2 is not prime: a digit of the prime is wrong")
	}
	if p.Bytes()[0] != 0xFF || p.Bytes()[len(p.Bytes())-1] != 0xFF {
		t.Error("the prime does not start and end with FF bytes")
	}
}

func TestTwoSidesOfTheKeyExchangeAgree(t *testing.T) {
	a, err := newDHKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	b, err := newDHKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	ka, err := a.sessionKey(b.public)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := b.sessionKey(a.public)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ka, kb) || len(ka) != 16 {
		t.Errorf("keys differ or are not 16 bytes: %x %x", ka, kb)
	}
	if len(a.public) != dhSize {
		t.Errorf("public key has %d bytes", len(a.public))
	}
}

func TestAServerKeyThatCannotBeRight(t *testing.T) {
	a, err := newDHKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	p := dhPrime()
	pm1 := new(big.Int).Sub(p, big.NewInt(1))
	tests := []struct {
		name string
		key  []byte
	}{
		{"empty", nil},
		{"zero", []byte{0}},
		{"one", []byte{1}},
		{"p minus one", pm1.Bytes()},
		{"p", p.Bytes()},
		{"too long", bytes.Repeat([]byte{0xFF}, dhSize+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := a.sessionKey(tt.key)
			wantRoute(t, err, "dhKeyPair.sessionKey.KeyringSessionFailed")
		})
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 16)
	for _, secret := range []string{"", "a", "0123456789abcde", "0123456789abcdef", "0123456789abcdefg", "pässwörd-日本語", string(bytes.Repeat([]byte("x"), 1000))} {
		iv, ct, err := encryptSecret(key, []byte(secret))
		if err != nil {
			t.Fatal(err)
		}
		if len(ct)%16 != 0 || len(ct) <= len(secret) {
			t.Errorf("ciphertext of %d bytes for a secret of %d", len(ct), len(secret))
		}
		got, err := decryptSecret(key, iv, ct)
		if err != nil || string(got) != secret {
			t.Errorf("round trip of %q = %q, %v", secret, got, err)
		}
	}
	iv1, ct1, err := encryptSecret(key, []byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	iv2, ct2, err := encryptSecret(key, []byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(iv1, iv2) || bytes.Equal(ct1, ct2) {
		t.Error("two encryptions of one secret are identical: the IV is not random")
	}
}

func TestDecryptRefusesWhatIsNotASecret(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 16)
	iv, ct, err := encryptSecret(key, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	// The cases below must fail on every run, so none of them may depend on the random IV: a wrong key or a damaged
	// block decrypts to random bytes, which end in valid padding about once in 256 runs. Flipping the last byte of the
	// IV flips the pad byte of a one-block secret (6 bytes, pad 10) to 0xF5, which is never valid; the other key gets a
	// fixed IV and ciphertext whose decryption under that key is known to end in an invalid pad.
	badPadIV := bytes.Clone(iv)
	badPadIV[len(badPadIV)-1] ^= 0xFF
	fixedIV := bytes.Repeat([]byte{3}, aes.BlockSize)
	fixedCT := make([]byte, aes.BlockSize)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	cipher.NewCBCEncrypter(block, fixedIV).CryptBlocks(fixedCT, append([]byte("secret"), bytes.Repeat([]byte{10}, 10)...))
	tests := []struct {
		name      string
		key       []byte
		iv, ct    []byte
		wantRoute string
	}{
		{"a short IV", key, iv[:8], ct, "decryptSecret.KeyringSecretInvalid"},
		{"no ciphertext", key, iv, nil, "decryptSecret.KeyringSecretInvalid"},
		{"a partial block", key, iv, ct[:len(ct)-1], "decryptSecret.KeyringSecretInvalid"},
		{"another key", bytes.Repeat([]byte{8}, 16), fixedIV, fixedCT, "decryptSecret.KeyringSecretInvalid"},
		{"damaged padding", key, badPadIV, ct, "decryptSecret.KeyringSecretInvalid"},
		{"a key of the wrong size", []byte{1, 2, 3}, iv, ct, "decryptSecret.KeyringCryptoFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decryptSecret(tt.key, tt.iv, tt.ct)
			wantRoute(t, err, tt.wantRoute)
		})
	}
	_, _, err = encryptSecret([]byte{1, 2, 3}, []byte("x"))
	wantRoute(t, err, "encryptSecret.KeyringCryptoFailed")
}
