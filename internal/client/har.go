package client

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ivorjpc/mercadona/internal/config"
)

// HARSession is the Mercadona auth material extracted from a browser HAR export
// (DevTools → Network → "Export HAR…"). Both login methods are recognised:
//   - email/password  → POST /api/auth/tokens/         {access_token, refresh_token, customer_id}
//   - Google sign-in  → POST /api/auth/social/google/  {access_token, refresh_token, customer_uuid}
//
// Only auth *response* bodies and request Bearer/Cookie *headers* are read; the
// request bodies — which hold the password — are deliberately never touched.
type HARSession struct {
	AccessToken  string
	RefreshToken string
	Cookie       string
	CustomerID   string
	LoginKind    string // "password", "google/social", or "headers"
}

// ParseHAR scans a HAR for the most recent Mercadona session: the access +
// refresh tokens from a /api/auth/ response, plus the freshest Bearer token and
// Cookie header seen on authenticated /api/ requests (a fallback when the login
// response itself wasn't captured, and the source of the Akamai cookie).
func ParseHAR(data []byte) (HARSession, error) {
	var har struct {
		Log struct {
			Entries []struct {
				Request struct {
					URL     string `json:"url"`
					Headers []struct {
						Name  string `json:"name"`
						Value string `json:"value"`
					} `json:"headers"`
				} `json:"request"`
				Response struct {
					Status  int `json:"status"`
					Content struct {
						Text     string `json:"text"`
						Encoding string `json:"encoding"`
					} `json:"content"`
				} `json:"response"`
			} `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal(data, &har); err != nil {
		return HARSession{}, fmt.Errorf("parse HAR JSON: %w", err)
	}

	var s HARSession
	for _, e := range har.Log.Entries {
		url := e.Request.URL

		// Auth responses carry the durable refresh token + an access token.
		if e.Response.Status == 200 && strings.Contains(url, "/api/auth/") {
			var r struct {
				AccessToken  string  `json:"access_token"`
				RefreshToken string  `json:"refresh_token"`
				CustomerID   flexStr `json:"customer_id"`
				CustomerUUID flexStr `json:"customer_uuid"`
			}
			if json.Unmarshal(harBody(e.Response.Content.Text, e.Response.Content.Encoding), &r) == nil {
				if r.AccessToken != "" {
					s.AccessToken = r.AccessToken
				}
				if r.RefreshToken != "" {
					s.RefreshToken = r.RefreshToken
				}
				if id := r.CustomerID.String(); id != "" {
					s.CustomerID = id
				}
				if id := r.CustomerUUID.String(); id != "" {
					s.CustomerID = id
				}
				if r.AccessToken != "" || r.RefreshToken != "" {
					if strings.Contains(url, "/social/") {
						s.LoginKind = "google/social"
					} else {
						s.LoginKind = "password"
					}
				}
			}
		}

		// Authenticated API requests carry the freshest Bearer token + Akamai cookie.
		if strings.Contains(url, "mercadona.es/api/") {
			for _, h := range e.Request.Headers {
				switch strings.ToLower(h.Name) {
				case "authorization":
					if v := strings.TrimSpace(h.Value); len(v) > 7 && strings.EqualFold(v[:7], "bearer ") {
						s.AccessToken = strings.TrimSpace(v[7:])
						if s.LoginKind == "" {
							s.LoginKind = "headers"
						}
					}
				case "cookie":
					if h.Value != "" {
						s.Cookie = h.Value
					}
				}
			}
		}
	}

	if s.CustomerID == "" && s.AccessToken != "" {
		s.CustomerID = customerFromJWT(s.AccessToken)
	}
	if s.AccessToken == "" && s.RefreshToken == "" && s.Cookie == "" {
		return s, fmt.Errorf("no Mercadona auth material found in HAR " +
			"(no /api/auth/ response, Bearer header, or cookie) — export the HAR while logged in")
	}
	return s, nil
}

// SaveHARSession caches the full session (access + refresh + cookie + customer)
// to the session file so it's usable immediately and can self-refresh.
func SaveHARSession(s HARSession) error {
	return config.Save(tokenCacheFile, Token{
		AccessToken:  s.AccessToken,
		RefreshToken: s.RefreshToken,
		Cookie:       s.Cookie,
		CustomerID:   flexStr(s.CustomerID),
	})
}

// harBody returns a HAR response body, decoding base64 when the HAR marked it so.
func harBody(text, encoding string) []byte {
	if encoding == "base64" {
		if b, err := base64.StdEncoding.DecodeString(text); err == nil {
			return b
		}
	}
	return []byte(text)
}
