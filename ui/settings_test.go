package ui

import (
	"dhtc/config"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSettingsPostDoesNotSaveFailedDownloader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	failedDownloader := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer failedDownloader.Close()

	configuration := &config.Configuration{TransmissionURL: "http://working.example/rpc", NetworkMode: "dual"}
	controller := &Controller{Configuration: configuration}
	router := gin.New()
	router.HTMLRender = loadTemplates()
	router.POST("/settings", controller.SettingsPost)

	form := url.Values{"TransmissionURL": []string{failedDownloader.URL}}
	request := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("got status %d, want %d", response.Code, http.StatusBadRequest)
	}
	if configuration.TransmissionURL != "http://working.example/rpc" {
		t.Fatalf("failed downloader was saved: %q", configuration.TransmissionURL)
	}
}
