package downloader

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTransmissionConnectionUsesSessionGet(t *testing.T) {
	var method string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "session")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var payload struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		method = payload.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"success"}`))
	}))
	defer server.Close()

	client := &TransmissionClient{URL: server.URL}
	if err := client.TestConnection(); err != nil {
		t.Fatal(err)
	}
	if method != "session-get" {
		t.Fatalf("got method %q, want session-get", method)
	}
}

func TestAria2ConnectionUsesGetVersion(t *testing.T) {
	var method string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		method = payload.Method
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"version":"1.0"}}`))
	}))
	defer server.Close()

	if err := (&Aria2Client{URL: server.URL}).TestConnection(); err != nil {
		t.Fatal(err)
	}
	if method != "aria2.getVersion" {
		t.Fatalf("got method %q, want aria2.getVersion", method)
	}
}

func TestDelugeConnectionRejectsFailedAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":1,"result":false,"error":null}`))
	}))
	defer server.Close()

	if err := (&DelugeClient{URL: server.URL, Pass: "wrong"}).TestConnection(); err == nil {
		t.Fatal("expected authentication error")
	}
}

func TestQBittorrentConnectionChecksVersion(t *testing.T) {
	versionChecked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "session"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/version":
			versionChecked = true
			_, _ = w.Write([]byte("5.0.0"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := (&QBittorrentClient{URL: server.URL, User: "user", Pass: "pass"}).TestConnection(); err != nil {
		t.Fatal(err)
	}
	if !versionChecked {
		t.Fatal("version endpoint was not checked")
	}
}
