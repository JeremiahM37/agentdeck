package push

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// This package implements RFC 8291 by hand rather than pulling a dependency, so
// the encryption has to be verified against the spec's own decryption rather
// than "it did not error". A browser is the only other thing that would notice.

// browserKeys mints a subscription the way a browser does: a P-256 keypair and a
// 16-byte auth secret.
func browserKeys(t *testing.T) (*ecdh.PrivateKey, []byte, Subscription) {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	return priv, auth, Subscription{
		Endpoint: "https://push.example/x",
		Keys: map[string]string{
			"p256dh": base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()),
			"auth":   base64.RawURLEncoding.EncodeToString(auth),
		},
	}
}

// decryptAes128gcm is the receiving half of RFC 8291, written from the spec so
// it is an independent check rather than a mirror of the code under test.
func decryptAes128gcm(t *testing.T, body []byte, priv *ecdh.PrivateKey, auth []byte) []byte {
	t.Helper()
	if len(body) < 21 {
		t.Fatalf("body too short: %d bytes", len(body))
	}
	salt := body[:16]
	recordSize := binary.BigEndian.Uint32(body[16:20])
	idLen := int(body[20])
	if recordSize != 4096 {
		t.Errorf("record size header: %d", recordSize)
	}
	if idLen != 65 {
		t.Fatalf("key id length: %d (an uncompressed P-256 point is 65 bytes)", idLen)
	}
	serverPubRaw := body[21 : 21+idLen]
	ciphertext := body[21+idLen:]

	serverPub, err := ecdh.P256().NewPublicKey(serverPubRaw)
	if err != nil {
		t.Fatalf("server key is not a valid P-256 point: %v", err)
	}
	shared, err := priv.ECDH(serverPub)
	if err != nil {
		t.Fatal(err)
	}
	info := append([]byte("WebPush: info\x00"), priv.PublicKey().Bytes()...)
	info = append(info, serverPubRaw...)
	prk := hkdfBytes(auth, shared, info, 32)
	cek := hkdfBytes(salt, prk, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce := hkdfBytes(salt, prk, []byte("Content-Encoding: nonce\x00"), 12)

	block, err := aes.NewCipher(cek)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("decryption failed — a browser would drop this notification: %v", err)
	}
	// aes128gcm requires a padding delimiter after the payload
	if len(plain) == 0 || plain[len(plain)-1] != 0x02 {
		t.Errorf("missing the 0x02 padding delimiter: %q", plain)
	}
	return plain[:len(plain)-1]
}

func TestEncryptedPayloadDecryptsWithTheSubscriptionKeys(t *testing.T) {
	priv, auth, sub := browserKeys(t)
	payload := []byte(`{"title":"Approval needed","body":"Bash: rm -rf build/"}`)

	body, err := encrypt(sub, payload)
	if err != nil {
		t.Fatal(err)
	}
	if got := decryptAes128gcm(t, body, priv, auth); string(got) != string(payload) {
		t.Fatalf("round trip lost the payload:\n got %q\nwant %q", got, payload)
	}
}

// A fresh salt and ephemeral key per message is what stops two notifications
// being linkable, and reusing a nonce with the same key is catastrophic for GCM.
func TestEachMessageGetsFreshSaltAndKey(t *testing.T) {
	_, _, sub := browserKeys(t)
	a, err := encrypt(sub, []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := encrypt(sub, []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	if string(a[:16]) == string(b[:16]) {
		t.Error("salt was reused between messages")
	}
	if string(a[21:86]) == string(b[21:86]) {
		t.Error("the ephemeral key was reused between messages")
	}
}

func TestEncryptRejectsMalformedSubscriptionKeys(t *testing.T) {
	for name, keys := range map[string]map[string]string{
		"no keys at all": {},
		"bad p256dh":     {"p256dh": "!!!not base64", "auth": "AAAAAAAAAAAAAAAAAAAAAA"},
		"short p256dh":   {"p256dh": "AAAA", "auth": "AAAAAAAAAAAAAAAAAAAAAA"},
	} {
		if _, err := encrypt(Subscription{Endpoint: "https://x", Keys: keys}, []byte("x")); err == nil {
			t.Errorf("%s should have been rejected", name)
		}
	}
}

// vapidPrivate is a P-256 scalar in the base64url form a generator emits.
func vapidPrivate(t *testing.T) (string, *ecdsa.PublicKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	d := make([]byte, 32)
	key.D.FillBytes(d)
	return base64.RawURLEncoding.EncodeToString(d), &key.PublicKey
}

// The VAPID JWT is what the push service checks before it will deliver anything.
func TestVAPIDJWTIsWellFormedAndVerifies(t *testing.T) {
	priv, pub := vapidPrivate(t)
	s := &Sender{PrivateKey: priv, PublicKey: "ignored-for-signing",
		Email: "admin@example.com", Log: slog.Default()}

	jwt, err := s.vapidJWT("https://fcm.googleapis.com/fcm/send/abc123")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("a JWT has three parts, got %d", len(parts))
	}
	var header struct{ Typ, Alg string }
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("header is not base64url: %v", err)
	}
	json.Unmarshal(raw, &header)
	if header.Alg != "ES256" || header.Typ != "JWT" {
		t.Errorf("header: %+v", header)
	}

	var claims struct {
		Aud string `json:"aud"`
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	raw, _ = base64.RawURLEncoding.DecodeString(parts[1])
	json.Unmarshal(raw, &claims)
	// the audience is the push service's ORIGIN, not the full endpoint — a
	// service rejects a token scoped to the wrong thing
	if claims.Aud != "https://fcm.googleapis.com" {
		t.Errorf("aud must be the origin only, got %q", claims.Aud)
	}
	if claims.Sub != "mailto:admin@example.com" {
		t.Errorf("sub: %q", claims.Sub)
	}
	if claims.Exp <= 0 {
		t.Error("exp must be set or the token is rejected outright")
	}

	// the signature must verify against the matching public key
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != 64 {
		t.Fatalf("ES256 signature must be 64 raw bytes, got %d (%v)", len(sig), err)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	r := new(big.Int).SetBytes(sig[:32])
	sv := new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(pub, sum[:], r, sv) {
		t.Fatal("the signature does not verify — a push service would reject this")
	}
}

func TestVAPIDRejectsAMalformedPrivateKey(t *testing.T) {
	s := &Sender{PrivateKey: "!!!not base64", Email: "a@b", Log: slog.Default()}
	if _, err := s.vapidJWT("https://x/y"); err == nil {
		t.Fatal("a malformed key must fail loudly, not sign garbage")
	}
}

func TestSendIsANoOpUntilConfigured(t *testing.T) {
	s := &Sender{Log: slog.Default()}
	if s.Enabled() {
		t.Error("push must not claim to be enabled without keys")
	}
	gone, err := s.Send(Subscription{Endpoint: "https://x"}, []byte("{}"))
	if gone || err != nil {
		t.Fatalf("unconfigured send should be a silent no-op: gone=%v err=%v", gone, err)
	}
}

// A subscription the browser has dropped answers 404/410, and the caller prunes
// it. Anything else must not look like a dead subscription.
func TestSendReportsGoneSubscriptions(t *testing.T) {
	priv, _ := vapidPrivate(t)
	_, _, sub := browserKeys(t)
	for status, wantGone := range map[int]bool{404: true, 410: true, 201: false, 500: false} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Content-Encoding") != "aes128gcm" {
				t.Errorf("missing content encoding header")
			}
			if !strings.HasPrefix(r.Header.Get("Authorization"), "vapid t=") {
				t.Errorf("missing VAPID authorization: %q", r.Header.Get("Authorization"))
			}
			io.Copy(io.Discard, r.Body)
			w.WriteHeader(status)
		}))
		s := &Sender{PrivateKey: priv, PublicKey: "k", Email: "a@b",
			Client: srv.Client(), Log: slog.Default()}
		sub.Endpoint = srv.URL
		gone, err := s.Send(sub, []byte(`{"title":"x"}`))
		if gone != wantGone {
			t.Errorf("status %d: gone=%v want %v", status, gone, wantGone)
		}
		if status >= 400 && !wantGone && err == nil {
			t.Errorf("status %d should surface an error", status)
		}
		srv.Close()
	}
}
