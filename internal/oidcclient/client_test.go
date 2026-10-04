package oidcclient

import "testing"

func TestValidateURL(t *testing.T) {
	for _, test := range []struct {
		name         string
		url          string
		requireHTTPS bool
		wantErr      bool
	}{
		{name: "secure endpoint", url: "https://idp.example/token", requireHTTPS: true},
		{name: "development HTTP endpoint", url: "http://idp:8080/token"},
		{name: "insecure production endpoint", url: "http://idp.example/token", requireHTTPS: true, wantErr: true},
		{name: "relative endpoint", url: "/token", wantErr: true},
		{name: "credentials in endpoint", url: "https://user:pass@idp.example/token", wantErr: true},
		{name: "fragment in endpoint", url: "https://idp.example/token#secret", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateURL(test.url, test.requireHTTPS)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateURL(%q) error = %v, want error %v", test.url, err, test.wantErr)
			}
		})
	}
}
