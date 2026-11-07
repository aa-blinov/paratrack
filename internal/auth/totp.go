package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP (RFC 6238) as authenticator apps read it: HMAC-SHA1, 30-second steps and
// six digits. The secret is 20 random bytes, shown to the person in base32.
const (
	totpStep   = 30 * time.Second
	totpDigits = 6
	// totpWindow accepts the code of the previous and the next step as well, so a
	// phone whose clock is half a minute off still gets in.
	totpWindow      = 1
	totpSecretBytes = 20
)

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret returns a fresh base32 secret for an authenticator app.
func newTOTPSecret() (string, error) {
	var b [totpSecretBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate totp secret: %w", err)
	}
	return totpEncoding.EncodeToString(b[:]), nil
}

// totpCodeAt is the six-digit code of one step, counted from the Unix epoch.
func totpCodeAt(secret []byte, step int64) string {
	mac := hmac.New(sha1.New, secret)
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	code := value % 1000000
	return fmt.Sprintf("%06d", code)
}

// verifyTOTP checks a typed code against the steps around now. It returns the
// step the code belongs to, so the caller can refuse a step it has already used.
func verifyTOTP(encodedSecret, code string, now time.Time) (int64, bool) {
	secret, err := totpEncoding.DecodeString(strings.ToUpper(strings.TrimSpace(encodedSecret)))
	if err != nil || len(secret) == 0 {
		return 0, false
	}
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0, false
	}
	current := now.Unix() / int64(totpStep/time.Second)
	for offset := int64(-totpWindow); offset <= totpWindow; offset++ {
		step := current + offset
		if step < 0 {
			continue
		}
		if hmac.Equal([]byte(totpCodeAt(secret, step)), []byte(code)) {
			return step, true
		}
	}
	return 0, false
}

// otpauthURI is the link an authenticator app opens on a phone. The issuer is the
// product name, so the app lists the account under it.
func otpauthURI(issuer, account, encodedSecret string) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	query := url.Values{}
	query.Set("secret", encodedSecret)
	query.Set("issuer", issuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", fmt.Sprint(totpDigits))
	query.Set("period", fmt.Sprint(int64(totpStep/time.Second)))
	return "otpauth://totp/" + label + "?" + query.Encode()
}
