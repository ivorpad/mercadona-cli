package client

import "testing"

// password login + later authenticated requests: the freshest Bearer header should
// win for the access token, the login response supplies the refresh token, and the
// cookie comes from the request headers.
func TestParseHAR_PasswordAndHeaders(t *testing.T) {
	har := `{"log":{"entries":[
	  {"request":{"url":"https://tienda.mercadona.es/api/auth/tokens/?lang=es&wh=mad1","headers":[]},
	   "response":{"status":200,"content":{"text":"{\"access_token\":\"AAA\",\"refresh_token\":\"RRR\",\"customer_id\":\"123\"}"}}},
	  {"request":{"url":"https://tienda.mercadona.es/api/customers/123/","headers":[
	     {"name":"Authorization","value":"Bearer BBB"},
	     {"name":"Cookie","value":"ck=1; foo=bar"}]},
	   "response":{"status":200,"content":{"text":"{}"}}}
	]}}`
	s, err := ParseHAR([]byte(har))
	if err != nil {
		t.Fatalf("ParseHAR: %v", err)
	}
	if s.RefreshToken != "RRR" {
		t.Errorf("refresh = %q, want RRR", s.RefreshToken)
	}
	if s.AccessToken != "BBB" { // freshest Bearer header beats the login-response token
		t.Errorf("access = %q, want BBB", s.AccessToken)
	}
	if s.Cookie != "ck=1; foo=bar" {
		t.Errorf("cookie = %q", s.Cookie)
	}
	if s.CustomerID != "123" {
		t.Errorf("customer = %q, want 123", s.CustomerID)
	}
	if s.LoginKind != "password" {
		t.Errorf("loginKind = %q, want password", s.LoginKind)
	}
}

// Google sign-in uses a different endpoint and reports the id as customer_uuid.
func TestParseHAR_GoogleSocial(t *testing.T) {
	har := `{"log":{"entries":[
	  {"request":{"url":"https://tienda.mercadona.es/api/auth/social/google/?lang=es&wh=mad1","headers":[]},
	   "response":{"status":200,"content":{"text":"{\"access_token\":\"GGG\",\"refresh_token\":\"GR\",\"customer_uuid\":\"uuid-9\"}"}}}
	]}}`
	s, err := ParseHAR([]byte(har))
	if err != nil {
		t.Fatalf("ParseHAR: %v", err)
	}
	if s.RefreshToken != "GR" || s.CustomerID != "uuid-9" || s.LoginKind != "google/social" {
		t.Errorf("got %+v", s)
	}
}

// base64-encoded response bodies (HAR allows it) must still be decoded.
func TestParseHAR_Base64Body(t *testing.T) {
	// base64 of {"access_token":"AAA","refresh_token":"RRR","customer_id":"77"}
	har := `{"log":{"entries":[
	  {"request":{"url":"https://tienda.mercadona.es/api/auth/tokens/","headers":[]},
	   "response":{"status":200,"content":{"encoding":"base64","text":"eyJhY2Nlc3NfdG9rZW4iOiJBQUEiLCJyZWZyZXNoX3Rva2VuIjoiUlJSIiwiY3VzdG9tZXJfaWQiOiI3NyJ9"}}}
	]}}`
	s, err := ParseHAR([]byte(har))
	if err != nil {
		t.Fatalf("ParseHAR: %v", err)
	}
	if s.RefreshToken != "RRR" || s.CustomerID != "77" {
		t.Errorf("got %+v", s)
	}
}

// a HAR with no Mercadona auth material is a clear error, not a silent empty session.
func TestParseHAR_Empty(t *testing.T) {
	if _, err := ParseHAR([]byte(`{"log":{"entries":[]}}`)); err == nil {
		t.Error("expected an error for a HAR with no auth material")
	}
}
