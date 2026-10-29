package dbconn

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"math/big"

	"github.com/rm4n0s/errors"
)

// The algorithms of a Secret Service session (the spec calls the second one "dh-ietf1024-sha256-aes128-cbc-pkcs7").
const (
	algorithmPlain = "plain"
	algorithmDH    = "dh-ietf1024-sha256-aes128-cbc-pkcs7"
)

// oakleyGroup2 is the prime of the second Oakley group (RFC 2409 section 6.2, 1024 bits), the group of the Secret Service key
// exchange; its generator is 2.
const oakleyGroup2 = "FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD129024E088A67CC74" +
	"020BBEA63B139B22514A08798E3404DDEF9519B3CD3A431B302B0A6DF25F1437" +
	"4FE1356D6D51C245E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7ED" +
	"EE386BFB5A899FA5AE9F24117C4B1FE649286651ECE65381FFFFFFFFFFFFFFFF"

// dhPrime returns the prime of the group.
func dhPrime() *big.Int {
	p, _ := new(big.Int).SetString(oakleyGroup2, 16)
	return p
}

// dhSize is the size in bytes of a public key and of the shared secret.
const dhSize = 128

// dhKeyPair is the client's half of the key exchange.
type dhKeyPair struct {
	private *big.Int
	public  []byte
}

// newDHKeyPair makes a private key and its public key g^x mod p, as 128 bytes.
func newDHKeyPair() (*dhKeyPair, error) {
	p := dhPrime()
	x, err := rand.Int(rand.Reader, new(big.Int).Sub(p, big.NewInt(3)))
	if err != nil {
		return nil, errors.NewErr("KeyringCryptoFailed", err).SetMetadata("step", "private key")
	}
	x.Add(x, big.NewInt(2)) // 2 <= x <= p-2
	pub := new(big.Int).Exp(big.NewInt(2), x, p)
	return &dhKeyPair{private: x, public: pub.FillBytes(make([]byte, dhSize))}, nil
}

// sessionKey derives the AES key from the server's public key: HKDF-SHA256 of the shared secret, with no salt and no info, 16
// bytes. The server's key must lie in 2..p-2 (a small key would give a guessable secret).
func (k *dhKeyPair) sessionKey(serverPublic []byte) ([]byte, error) {
	p := dhPrime()
	y := new(big.Int).SetBytes(serverPublic)
	if len(serverPublic) == 0 || len(serverPublic) > dhSize || y.Cmp(big.NewInt(2)) < 0 || y.Cmp(new(big.Int).Sub(p, big.NewInt(2))) > 0 {
		return nil, errors.New("KeyringSessionFailed", "the key the service sent for the session is not valid", "length", len(serverPublic))
	}
	shared := new(big.Int).Exp(y, k.private, p).FillBytes(make([]byte, dhSize))
	key, err := hkdf.Key(sha256.New, shared, nil, "", 16)
	if err != nil {
		return nil, errors.NewErr("KeyringCryptoFailed", err).SetMetadata("step", "hkdf")
	}
	return key, nil
}

// encryptSecret encrypts the secret with AES-128-CBC and PKCS#7 padding under a new random IV; it returns the IV and the
// ciphertext.
func encryptSecret(key, secret []byte) (iv, ciphertext []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, errors.NewErr("KeyringCryptoFailed", err).SetMetadata("step", "cipher")
	}
	iv = make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return nil, nil, errors.NewErr("KeyringCryptoFailed", err).SetMetadata("step", "iv")
	}
	pad := aes.BlockSize - len(secret)%aes.BlockSize
	padded := append(bytes.Clone(secret), bytes.Repeat([]byte{byte(pad)}, pad)...)
	ciphertext = make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)
	return iv, ciphertext, nil
}

// decryptSecret undoes encryptSecret.
func decryptSecret(key, iv, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.NewErr("KeyringCryptoFailed", err).SetMetadata("step", "cipher")
	}
	if len(iv) != aes.BlockSize || len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, errors.New("KeyringSecretInvalid", "the secret the service sent is not a whole number of blocks", "iv", len(iv), "length", len(ciphertext))
	}
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)
	pad := int(plain[len(plain)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(plain) {
		return nil, errors.New("KeyringSecretInvalid", "the padding of the secret the service sent is not valid", "pad", pad)
	}
	for _, b := range plain[len(plain)-pad:] {
		if int(b) != pad {
			return nil, errors.New("KeyringSecretInvalid", "the padding of the secret the service sent is not valid", "pad", pad)
		}
	}
	return plain[:len(plain)-pad], nil
}
