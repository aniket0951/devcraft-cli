package config

import "testing"

func TestLoad_Precedence(t *testing.T) {
	t.Setenv(EnvAPIURL, "")
	cfg, err := Load("")
	if err != nil || cfg.APIBaseURL != DefaultAPIBaseURL {
		t.Fatalf("default: cfg=%+v err=%v", cfg, err)
	}

	t.Setenv(EnvAPIURL, "https://env.example.com/")
	cfg, _ = Load("")
	if cfg.APIBaseURL != "https://env.example.com" {
		t.Fatalf("env: %s", cfg.APIBaseURL)
	}

	cfg, _ = Load("https://flag.example.com")
	if cfg.APIBaseURL != "https://flag.example.com" {
		t.Fatalf("flag: %s", cfg.APIBaseURL)
	}
}

func TestLoad_Validation(t *testing.T) {
	t.Setenv(EnvAPIURL, "")
	ok := []string{"https://api.example.com", "http://localhost:8080", "http://127.0.0.1:9000", "http://[::1]:80"}
	bad := []string{"http://api.example.com", "ftp://x.com", "not a url", "https://"}

	for _, u := range ok {
		if _, err := Load(u); err != nil {
			t.Errorf("Load(%q) err = %v", u, err)
		}
	}
	for _, u := range bad {
		if _, err := Load(u); err == nil {
			t.Errorf("Load(%q) expected error", u)
		}
	}
}
