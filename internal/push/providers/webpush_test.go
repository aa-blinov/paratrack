package providers

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/aa-blinov/paratrack/internal/pushport"
)

type closeTrackingBody struct {
	io.ReadCloser
	closed bool
}

func (b *closeTrackingBody) Close() error {
	b.closed = true
	return b.ReadCloser.Close()
}

type responseTrackingTransport struct {
	base http.RoundTripper
	body *closeTrackingBody
}

func (t *responseTrackingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err == nil && response != nil && response.Body != nil {
		t.body = &closeTrackingBody{ReadCloser: response.Body}
		response.Body = t.body
	}
	return response, err
}

func TestSendEncryptsPushAndMapsExpiredStatuses(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusCode int
		expired    bool
	}{
		{name: "accepted", statusCode: http.StatusCreated},
		{name: "gone", statusCode: http.StatusGone, expired: true},
		{name: "not found", statusCode: http.StatusNotFound, expired: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ts := newPushTestServer(t, test.statusCode)
			defer ts.Close()

			privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
			if err != nil {
				t.Fatal(err)
			}
			receiverKey, err := ecdh.P256().GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			authKey := make([]byte, 16)
			if _, err := rand.Read(authKey); err != nil {
				t.Fatal(err)
			}

			client := ts.Client()
			transport := &responseTrackingTransport{base: client.Transport}
			client.Transport = transport
			sender, err := New(client)
			if err != nil {
				t.Fatal(err)
			}
			result, err := sender.Send(context.Background(), pushport.Subscription{
				Endpoint: ts.URL,
				P256DH:   base64.RawURLEncoding.EncodeToString(receiverKey.PublicKey().Bytes()),
				Auth:     base64.RawURLEncoding.EncodeToString(authKey),
			}, publicKey, privateKey, []byte(`{"title":"done"}`))
			if err != nil {
				t.Fatal(err)
			}
			if result.SubscriptionExpired != test.expired {
				t.Fatalf("SubscriptionExpired = %v, want %v", result.SubscriptionExpired, test.expired)
			}
			if !transport.body.closed {
				t.Fatal("Web Push response body was not closed")
			}
		})
	}
}

func newPushTestServer(t *testing.T, statusCode int) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", request.Method)
		}
		if request.Header.Get("Content-Encoding") != "aes128gcm" {
			t.Errorf("Content-Encoding = %q, want aes128gcm", request.Header.Get("Content-Encoding"))
		}
		if !strings.HasPrefix(request.Header.Get("Authorization"), "vapid ") {
			t.Errorf("Authorization = %q, want VAPID token", request.Header.Get("Authorization"))
		}
		if request.Header.Get("TTL") != "86400" {
			t.Errorf("TTL = %q, want 86400", request.Header.Get("TTL"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read encrypted request body: %v", err)
		} else if len(body) == 0 || bytes.Equal(body, []byte(`{"title":"done"}`)) {
			t.Errorf("request body is empty or contains plaintext: %q", body)
		}
		w.WriteHeader(statusCode)
	}))
}
