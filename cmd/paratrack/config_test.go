package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadProcessConfigReadsEachEnvironmentSettingOnce(t *testing.T) {
	values := map[string]string{
		"PARATRACK_ENV":             "test",
		"PARATRACK_DATABASE_URL":    "postgres://test.invalid/db",
		"PARATRACK_SECRET_KEY":      " test-key ",
		"PARATRACK_STRIPE_KEY":      " stripe-key ",
		"PARATRACK_PUBLIC_URL":      "http://localhost:8080/",
		"PARATRACK_TRUSTED_PROXIES": "127.0.0.1/32",
	}
	reads := make(map[string]int)
	config, err := loadProcessConfigWith(func(key string) string {
		reads[key]++
		return values[key]
	})
	if err != nil {
		t.Fatalf("load process config: %v", err)
	}
	for key, count := range reads {
		if count != 1 {
			t.Errorf("environment setting %s read %d times, want once", key, count)
		}
	}
	if config.database.URL != values["PARATRACK_DATABASE_URL"] || config.database.SecretKey != "test-key" || config.database.RequireSecretKey {
		t.Errorf("database config = %+v", config.database)
	}
	if config.services.DefaultTimezone != time.Local.String() || config.web.DefaultTimezone != config.services.DefaultTimezone {
		t.Errorf("default timezones differ or were not resolved: app=%q web=%q", config.services.DefaultTimezone, config.web.DefaultTimezone)
	}
	if config.services.StripeAPIKey != "stripe-key" || config.web.PublicURL != "http://localhost:8080" {
		t.Errorf("parsed settings = services:%+v web:%+v", config.services, config.web)
	}
}

func TestLoadProcessConfigRejectsInvalidTimezone(t *testing.T) {
	_, err := loadProcessConfigWith(func(key string) string {
		if key == "PARATRACK_ENV" {
			return "test"
		}
		if key == "PARATRACK_TZ" {
			return "Not/A_Real_Zone"
		}
		return ""
	})
	if err == nil {
		t.Fatal("loadProcessConfigWith accepted an invalid timezone")
	}
}

func TestLoadCLITimezoneUsesConfiguredLocation(t *testing.T) {
	location, err := loadCLITimezoneWith(func(key string) string {
		if key == "PARATRACK_TZ" {
			return "Asia/Tokyo"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if location.String() != "Asia/Tokyo" {
		t.Fatalf("CLI timezone = %q, want Asia/Tokyo", location)
	}
}

func TestLoadProcessConfigWithTimezoneDoesNotRereadTimezone(t *testing.T) {
	location, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	reads := make(map[string]int)
	values := map[string]string{
		"PARATRACK_ENV":        "test",
		"PARATRACK_PUBLIC_URL": "http://localhost:8080",
	}
	config, err := loadProcessConfigWithTimezone(func(key string) string {
		reads[key]++
		return values[key]
	}, location)
	if err != nil {
		t.Fatalf("load process config: %v", err)
	}
	if reads["PARATRACK_TZ"] != 0 {
		t.Fatalf("PARATRACK_TZ read %d times, want the existing timezone snapshot", reads["PARATRACK_TZ"])
	}
	if config.services.DefaultTimezone != location.String() || config.web.DefaultTimezone != location.String() {
		t.Fatalf("configured timezone = services:%q web:%q, want %q", config.services.DefaultTimezone, config.web.DefaultTimezone, location)
	}
}

// A token only goes to the address that came with it: a `.paratrack` file in a
// working copy cannot point the user's own token at another server.
func TestRemoteConfigNeverPairsATokenWithAnotherFile(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "config.json")
	if err := os.WriteFile(home, []byte(`{"url":"https://home.test","token":"pt_home"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(dir, "work")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, ".paratrack"), []byte(`{"url":"https://evil.test"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)
	t.Setenv("PARATRACK_URL", "")
	t.Setenv("PARATRACK_TOKEN", "")

	url, token := loadCLIRemoteConfig(home)
	if url != "https://home.test" || token != "pt_home" {
		t.Fatalf("config = %q %q, want the home pair", url, token)
	}
	// A project file that carries both is the whole pair, and a file with only
	// a token names no server to send it to.
	if err := os.WriteFile(filepath.Join(work, ".paratrack"), []byte(`{"url":"https://studio.test","token":"pt_project"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if url, token := loadCLIRemoteConfig(home); url != "https://studio.test" || token != "pt_project" {
		t.Fatalf("project config = %q %q, want its own pair", url, token)
	}
	if err := os.WriteFile(filepath.Join(work, ".paratrack"), []byte(`{"token":"pt_orphan"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if url, token := loadCLIRemoteConfig(home); url != "https://home.test" || token != "pt_home" {
		t.Fatalf("orphan token = %q %q, want the home pair", url, token)
	}
}
