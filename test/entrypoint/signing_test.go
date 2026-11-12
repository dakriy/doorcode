package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// signingKeySetting is where the store keeps the firmware signing key.
const signingKeySetting = "firmware_signing_private_key"

// The firmware signing key a new site makes from the stub's entropy: the
// 40 bytes 0x22 to 0x49, drawn after the first administrator's password
// and salt, reduced modulo the P-256 order less one, plus one. The
// public key is its point x || y in hex, and the fingerprint the first 16
// hex characters of the SHA-256 of that point.
const (
	signingPrivateKey  = "50525455e5e4e3e33b25bccf2a981912fefdc17e227760793e991e37a7de29ea"
	signingFingerprint = "6b29a4c924997018"
	signingPublicKey   = "a96bfef5330bf8805f3cada848f9ff9f14645e6378af18d5968ea4e095b8c0279254e2db6fc2cb2a405ed08d6489b256e0c32aa4b8d094c0b3a564a39a147147"
)

// otherSecretKey is a valid key for the secret settings other than the one
// the stub environment sets.
const otherSecretKey = "ZmVkY2JhOTg3NjU0MzIxMGZlZGNiYTk4NzY1NDMyMTA="

// storedSigningKey is the firmware signing key f's store holds, decrypted,
// or why it cannot be.
func storedSigningKey(f *testsupport.Factory) []string {
	key, err := testsupport.OpenSecret(signingKeySetting, f.StoreStub.StoredSetting(signingKeySetting))
	return []string{key, testsupport.ErrorText(err)}
}

func TestFirstStartStoresASigningKeyEncrypted(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()

	start(t, f)

	if diff := cmp.Diff([]string{signingPrivateKey, "<nil>"}, storedSigningKey(f)); diff != "" {
		t.Errorf("stored key, error (-want +got):\n%s", diff)
	}
}

func TestARestartKeepsTheSigningKey(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	start(t, f)

	b := signedInAdmin(t, f)

	got := []any{storedSigningKey(f), testsupport.JSONAt(b.Get("/api/v1/firmware").Body, "/signing_key_fingerprint")}
	if diff := cmp.Diff([]any{[]string{signingPrivateKey, "<nil>"}, signingFingerprint}, got); diff != "" {
		t.Errorf("stored key, fingerprint (-want +got):\n%s", diff)
	}
}

func TestStartupFailsWhenTheStoredSigningKeyCannotBeDecrypted(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	start(t, f)
	stored := f.StoreStub.StoredSetting(signingKeySetting)
	f.EnvStub["DOORCODE_SECRET_KEY"] = otherSecretKey

	_, err := app.New(t.Context(), f)

	want := []string{
		"firmware signing key: the stored firmware signing key cannot be decrypted with the key in DOORCODE_SECRET_KEY; " +
			"start with the DOORCODE_SECRET_KEY it was stored under",
		stored,
	}
	if diff := cmp.Diff(want, []string{testsupport.ErrorText(err), f.StoreStub.StoredSetting(signingKeySetting)}); diff != "" {
		t.Errorf("error, stored key (-want +got):\n%s", diff)
	}
}

func TestTheSigningKeyIsNoSetting(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)

	read := b.Get("/api/v1/settings/" + signingKeySetting)
	changed := b.PutJSON("/api/v1/settings/"+signingKeySetting, `{"value":"changed"}`)

	want := []any{404, 404, []string{signingPrivateKey, "<nil>"}}
	if diff := cmp.Diff(want, []any{read.Status, changed.Status, storedSigningKey(f)}); diff != "" {
		t.Errorf("read, change, stored key (-want +got):\n%s", diff)
	}
}
