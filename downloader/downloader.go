package downloader

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client interface {
	AddMagnet(magnet string) error
}

type TestableClient interface {
	Client
	TestConnection() error
}

type TransmissionClient struct {
	URL       string
	User      string
	Pass      string
	SessionID string
}

func (c *TransmissionClient) TestConnection() error {
	payload := map[string]any{"method": "session-get"}
	return c.call(payload)
}

func (c *TransmissionClient) AddMagnet(magnet string) error {
	payload := map[string]any{
		"method": "torrent-add",
		"arguments": map[string]any{
			"filename": magnet,
		},
	}
	return c.call(payload)
}

func (c *TransmissionClient) call(payload map[string]any) error {
	for range 2 {
		err := func() error {
			body, _ := json.Marshal(payload)
			req, err := http.NewRequest("POST", c.URL, bytes.NewBuffer(body))
			if err != nil {
				return err
			}

			if c.User != "" {
				req.SetBasicAuth(c.User, c.Pass)
			}
			if c.SessionID != "" {
				req.Header.Set("X-Transmission-Session-Id", c.SessionID)
			}

			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusConflict {
				c.SessionID = resp.Header.Get("X-Transmission-Session-Id")
				return fmt.Errorf("conflict")
			}

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("transmission returned status %d", resp.StatusCode)
			}

			return nil
		}()

		if err == nil {
			return nil
		}

		if err.Error() == "conflict" {
			continue
		}

		return err
	}

	return fmt.Errorf("failed to get transmission session id")
}

type Aria2Client struct {
	URL   string
	Token string
}

func (c *Aria2Client) TestConnection() error {
	return c.call("aria2.getVersion", nil)
}

func (c *Aria2Client) AddMagnet(magnet string) error {
	return c.call("aria2.addUri", []any{[]string{magnet}})
}

func (c *Aria2Client) call(method string, methodParams []any) error {
	var params []any
	if c.Token != "" {
		params = append(params, "token:"+c.Token)
	}
	params = append(params, methodParams...)

	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      "dhtc",
		"method":  method,
		"params":  params,
	}

	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(c.URL, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("aria2 returned status %d: %s", resp.StatusCode, string(b))
	}
	var result struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("aria2 returned an invalid response: %w", err)
	}
	if len(result.Error) > 0 && string(result.Error) != "null" {
		return fmt.Errorf("aria2 RPC error: %s", result.Error)
	}

	return nil
}

type DelugeClient struct {
	URL  string
	Pass string
}

func (c *DelugeClient) TestConnection() error {
	_, err := c.login()
	return err
}

func (c *DelugeClient) AddMagnet(magnet string) error {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	cookies, err := c.login()
	if err != nil {
		return err
	}

	// Add magnet with the authenticated Deluge session.
	addPayload := map[string]any{
		"id":     2,
		"method": "core.add_torrent_magnet",
		"params": []any{magnet, map[string]any{}},
	}
	addBody, _ := json.Marshal(addPayload)
	req, err := http.NewRequest("POST", c.URL, bytes.NewBuffer(addBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("deluge add magnet failed with status %d", resp.StatusCode)
	}

	return nil
}

func (c *DelugeClient) login() ([]*http.Cookie, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	loginPayload := map[string]any{
		"id":     1,
		"method": "auth.login",
		"params": []string{c.Pass},
	}
	loginBody, _ := json.Marshal(loginPayload)
	resp, err := client.Post(c.URL, "application/json", bytes.NewBuffer(loginBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deluge login failed with status %d", resp.StatusCode)
	}
	var loginResult struct {
		Result bool            `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&loginResult); err != nil {
		return nil, fmt.Errorf("deluge returned an invalid login response: %w", err)
	}
	if len(loginResult.Error) > 0 && string(loginResult.Error) != "null" {
		return nil, fmt.Errorf("deluge login error: %s", loginResult.Error)
	}
	if !loginResult.Result {
		return nil, fmt.Errorf("deluge authentication failed")
	}

	var cookies []*http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "_session_id" {
			cookies = append(cookies, cookie)
		}
	}

	return cookies, nil
}

type QBittorrentClient struct {
	URL  string
	User string
	Pass string
}

func (c *QBittorrentClient) TestConnection() error {
	client, cookies, err := c.login()
	if err != nil {
		return err
	}
	req, err := http.NewRequest("GET", strings.TrimRight(c.URL, "/")+"/api/v2/app/version", nil)
	if err != nil {
		return err
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qbittorrent version check failed with status %d", resp.StatusCode)
	}
	return nil
}

func (c *QBittorrentClient) AddMagnet(magnet string) error {
	client, cookies, err := c.login()
	if err != nil {
		return err
	}

	addURL := strings.TrimRight(c.URL, "/") + "/api/v2/torrents/add"
	addData := url.Values{"urls": []string{magnet}}.Encode()
	req, err := http.NewRequest("POST", addURL, bytes.NewBufferString(addData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qbittorrent add magnet failed with status %d", resp.StatusCode)
	}

	return nil
}

func (c *QBittorrentClient) login() (*http.Client, []*http.Cookie, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	loginURL := strings.TrimRight(c.URL, "/") + "/api/v2/auth/login"
	loginData := url.Values{"username": []string{c.User}, "password": []string{c.Pass}}.Encode()
	resp, err := client.Post(loginURL, "application/x-www-form-urlencoded", bytes.NewBufferString(loginData))
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(responseBody)) != "Ok." {
		return nil, nil, fmt.Errorf("qbittorrent authentication failed")
	}
	var cookies []*http.Cookie
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "SID" {
			cookies = append(cookies, cookie)
		}
	}
	if len(cookies) == 0 {
		return nil, nil, fmt.Errorf("qbittorrent login did not return a session")
	}
	return client, cookies, nil
}
