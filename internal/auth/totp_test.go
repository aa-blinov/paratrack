package auth

import (
	"net/url"
	"testing"
	"time"
)

// The RFC 6238 appendix vectors for SHA1: the secret is the ASCII string below, and the
// eight-digit codes are shortened to the six digits an authenticator app shows.
const rfcSecretBase32 = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestTOTPMatchesTheRFCVectors(t *testing.T) {
	vectors := []struct {
		at   int64
		code string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	}
	for _, v := range vectors {
		step, ok := verifyTOTP(rfcSecretBase32, v.code, time.Unix(v.at, 0))
		if !ok {
			t.Fatalf("time %d: code %s refused", v.at, v.code)
		}
		if step != v.at/30 {
			t.Fatalf("time %d: step = %d, want %d", v.at, step, v.at/30)
		}
	}
}

func TestTOTPAcceptsOneStepEitherWayOnly(t *testing.T) {
	at := time.Unix(1111111109, 0)
	if _, ok := verifyTOTP(rfcSecretBase32, "081804", at.Add(30*time.Second)); !ok {
		t.Fatal("the previous step's code should be accepted one step later")
	}
	if _, ok := verifyTOTP(rfcSecretBase32, "081804", at.Add(2*time.Minute)); ok {
		t.Fatal("a code two minutes old should be refused")
	}
}

func TestTOTPRefusesMalformedInput(t *testing.T) {
	at := time.Unix(59, 0)
	for _, code := range []string{"", "28708", "2870821", "abcdef", " "} {
		if _, ok := verifyTOTP(rfcSecretBase32, code, at); ok {
			t.Fatalf("code %q accepted", code)
		}
	}
	if _, ok := verifyTOTP("not base32!", "287082", at); ok {
		t.Fatal("a secret that is not base32 was accepted")
	}
}

func TestNewTOTPSecretIsBase32AndRandom(t *testing.T) {
	first, err := newTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 32 || first == second {
		t.Fatalf("secrets %q and %q: want 32 characters and no repeat", first, second)
	}
}

func TestOTPAuthURIKeepsTheIssuerAndTheSecret(t *testing.T) {
	uri, err := url.Parse(otpauthURI("paratrack", "ivan@example.test", rfcSecretBase32))
	if err != nil {
		t.Fatal(err)
	}
	if uri.Scheme != "otpauth" || uri.Host != "totp" || uri.Path != "/paratrack:ivan@example.test" {
		t.Fatalf("label = %s://%s%s, want the issuer and the account", uri.Scheme, uri.Host, uri.Path)
	}
	query := uri.Query()
	want := map[string]string{"secret": rfcSecretBase32, "issuer": "paratrack", "digits": "6", "period": "30", "algorithm": "SHA1"}
	for key, value := range want {
		if query.Get(key) != value {
			t.Fatalf("query %s = %q, want %q", key, query.Get(key), value)
		}
	}
}
