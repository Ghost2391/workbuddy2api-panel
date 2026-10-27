package upstream

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBaseSemver(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"5.8.3.41334564", "5.8.3"},
		{"5.8.4.41366630", "5.8.4"},
		{"5.5.4", "5.5.4"},
		{"5.6", "5.6"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := baseSemver(tc.in); got != tc.want {
			t.Errorf("baseSemver(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsSemverOutdated(t *testing.T) {
	cases := []struct {
		cur  string
		tgt  string
		want bool
	}{
		{"5.5.4", "5.8.3.41334564", true},
		{"5.5.4", "5.8.4", true},
		{"5.8.3", "5.8.3", false},
		{"5.8.4", "5.8.3", false},
		{"6.0.0", "5.8.4", false},
		{"5.5.2", "5.5.4", true},
		{"5.5.4", "5.5.4", false},
	}
	for _, tc := range cases {
		if got := isSemverOutdated(tc.cur, tc.tgt); got != tc.want {
			t.Errorf("isSemverOutdated(%q, %q) = %v, want %v", tc.cur, tc.tgt, got, tc.want)
		}
	}
}

func TestFetchOfficialUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Product") != "SaaS" {
			t.Errorf("X-Product header = %q want SaaS", r.Header.Get("X-Product"))
		}
		json.NewEncoder(w).Encode(OfficialUpdateInfo{
			Version:   "5.8.3.41334564",
			URL:       "https://download.example/wb.exe",
			Timestamp: 1792057764,
		})
	}))
	defer srv.Close()

	info, err := fetchOfficialUpdate(srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("fetchOfficialUpdate: %v", err)
	}
	if info.Version != "5.8.3.41334564" {
		t.Errorf("version = %q want 5.8.3.41334564", info.Version)
	}
}
