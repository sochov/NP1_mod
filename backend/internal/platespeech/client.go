package platespeech

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultAPIBase is the WEB版VOICEVOX API (https://voicevox.su-shiki.com/su-shikiapis/).
const DefaultAPIBase = "https://deprecatedapis.tts.quest"

// APIError is a failure reported by the API itself.
type APIError struct {
	Code   string // invalidApiKey, notEnoughPoints, failed, ...
	Status int
}

func (e *APIError) Error() string { return fmt.Sprintf("VOICEVOX API: %s (%d)", e.Code, e.Status) }

// Permanent reports errors that retrying cannot fix without owner action.
func (e *APIError) Permanent() bool { return e.Code == "invalidApiKey" }

// API is the owner-configured endpoint and key (key empty until set).
type API struct {
	Endpoint string `json:"endpoint"`
	Key      string `json:"key"`
}

// ValidateEndpoint normalizes an API base URL: http(s), host, no
// credentials, query or fragment, no trailing slash.
func ValidateEndpoint(raw string) (string, error) {
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("エンドポイントは http(s):// で始まるURLで指定してください")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// Client calls the API. Only rule messages, the fixed phrase and plate
// vocabulary are sent; never a detected vehicle's plate.
type Client struct {
	HTTP *http.Client
}

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) post(ctx context.Context, base, path string, form url.Values, limit int64) (*http.Response, []byte, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, base+path, strings.NewReader(form.Encode()))
	if e != nil {
		return nil, nil, e
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "NP1-Control-Center")
	res, e := c.HTTP.Do(req)
	if e != nil {
		return nil, nil, e
	}
	defer res.Body.Close()
	body, e := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if e == nil && int64(len(body)) > limit {
		e = errors.New("VOICEVOX API response too large")
	}
	return res, body, e
}

func apiError(res *http.Response, body []byte) error {
	var v struct {
		ErrorMessage string `json:"errorMessage"`
	}
	if json.Unmarshal(body, &v) == nil && v.ErrorMessage != "" {
		return &APIError{Code: v.ErrorMessage, Status: res.StatusCode}
	}
	return &APIError{Code: "http-" + strconv.Itoa(res.StatusCode), Status: res.StatusCode}
}

// Synthesize returns the PCM of text in the given style.
func (c *Client) Synthesize(ctx context.Context, api API, text string, style int) ([]byte, error) {
	// This provider accepts text/key by POST, but speaker only by GET.
	// A form-body speaker is silently ignored and produces style 0 (Metan).
	form := url.Values{"key": {api.Key}, "text": {text}}
	res, body, e := c.post(ctx, api.Endpoint, "/v2/voicevox/audio/?speaker="+strconv.Itoa(style), form, MaxClipBytes+4096)
	if e != nil {
		return nil, e
	}
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "audio/") {
		return nil, apiError(res, body)
	}
	return ParseWAV(body)
}

// Style is one selectable voice.
type Style struct {
	ID      int    `json:"id"`
	Speaker string `json:"speaker"`
	Name    string `json:"name"`
}

// Styles lists the talk styles the API offers.
func (c *Client) Styles(ctx context.Context, api API) ([]Style, error) {
	res, body, e := c.post(ctx, api.Endpoint, "/v2/voicevox/speakers/", url.Values{"key": {api.Key}}, 1<<20)
	if e != nil {
		return nil, e
	}
	if res.StatusCode != http.StatusOK {
		return nil, apiError(res, body)
	}
	var speakers []struct {
		Name   string `json:"name"`
		Styles []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"styles"`
	}
	if e = json.Unmarshal(body, &speakers); e != nil {
		return nil, e
	}
	out := []Style{}
	for _, sp := range speakers {
		for _, st := range sp.Styles {
			if st.Type == "" || st.Type == "talk" {
				out = append(out, Style{ID: st.ID, Speaker: sp.Name, Name: st.Name})
			}
		}
	}
	return out, nil
}

// Points is the remaining daily allowance reported by the API.
type Points struct {
	Points       int64   `json:"points"`
	ResetInHours float64 `json:"resetInHours"`
}

func (c *Client) Points(ctx context.Context, api API) (Points, error) {
	var p Points
	res, body, e := c.post(ctx, api.Endpoint, "/v2/api/", url.Values{"key": {api.Key}}, 64<<10)
	if e != nil {
		return p, e
	}
	if res.StatusCode != http.StatusOK {
		return p, apiError(res, body)
	}
	if e = json.Unmarshal(body, &p); e != nil {
		return p, errors.New("VOICEVOX API: unexpected points response")
	}
	return p, nil
}
