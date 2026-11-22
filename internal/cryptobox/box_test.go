package cryptobox

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testKey() []byte {
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

func TestSealOpenRoundTrip(t *testing.T) {
	box, err := New(testKey())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sealed, err := box.Seal([]byte("developer-code"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bytes.Contains(sealed, []byte("developer-code")) {
		t.Fatal("sealed value contains plaintext")
	}

	plaintext, err := box.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(plaintext) != "developer-code" {
		t.Fatalf("plaintext = %q", plaintext)
	}
}

func TestOpenRejectsTamperingAndWrongKey(t *testing.T) {
	box, err := New(testKey())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sealed, err := box.Seal([]byte("secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := box.Open(tampered); err == nil {
		t.Fatal("Open accepted a tampered value")
	}

	other := testKey()
	other[0] ^= 0xff
	otherBox, err := New(other)
	if err != nil {
		t.Fatalf("New(other): %v", err)
	}
	if _, err := otherBox.Open(sealed); err == nil {
		t.Fatal("Open accepted the wrong key")
	}
}

func TestParseKey(t *testing.T) {
	key := testKey()
	for name, encoded := range map[string]string{
		"hex":        hex.EncodeToString(key),
		"base64":     base64.StdEncoding.EncodeToString(key),
		"raw base64": base64.RawStdEncoding.EncodeToString(key),
		"url base64": base64.RawURLEncoding.EncodeToString(key),
	} {
		t.Run(name, func(t *testing.T) {
			decoded, err := ParseKey(encoded)
			if err != nil {
				t.Fatalf("ParseKey: %v", err)
			}
			if !bytes.Equal(decoded, key) {
				t.Fatal("decoded key differs")
			}
		})
	}

	for _, raw := range []string{"", "short", strings.Repeat("x", 64)} {
		if _, err := ParseKey(raw); err == nil {
			t.Errorf("ParseKey(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestLoadOrCreateKeyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chastisafe.key")

	first, err := LoadOrCreate("", path)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("key file mode = %o, want 600", mode)
	}

	second, err := LoadOrCreate("", path)
	if err != nil {
		t.Fatalf("LoadOrCreate again: %v", err)
	}
	sealed, err := first.Seal([]byte("value"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := second.Open(sealed); err != nil {
		t.Fatalf("Open with reloaded key: %v", err)
	}
}

func TestLoadOrCreateUsesEnvironmentKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ignored.key")
	encoded := base64.RawStdEncoding.EncodeToString(testKey())

	box, err := LoadOrCreate(encoded, path)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("key file was created despite an environment key: %v", err)
	}
	sealed, err := box.Seal([]byte("value"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := box.Open(sealed); err != nil {
		t.Fatalf("Open: %v", err)
	}
}
