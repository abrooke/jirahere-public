package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type oauthRoundTripper func(*http.Request) (*http.Response, error)

func (f oauthRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type failingReadCloser struct{ err error }

func (b failingReadCloser) Read([]byte) (int, error) { return 0, b.err }
func (failingReadCloser) Close() error               { return nil }

func TestParseCallbackParams_Valid(t *testing.T) {
	q := url.Values{"state": {"abc"}, "code": {"the-code"}}
	code, err := parseCallbackParams(q, "abc")
	if err != nil {
		t.Fatalf("parseCallbackParams: %v", err)
	}
	if code != "the-code" {
		t.Errorf("code = %q, want %q", code, "the-code")
	}
}

func TestParseCallbackParams_ErrorParam(t *testing.T) {
	q := url.Values{"error": {"access_denied"}, "state": {"abc"}}
	_, err := parseCallbackParams(q, "abc")
	if !errors.Is(err, ErrLoginDenied) {
		t.Fatalf("err = %v, want ErrLoginDenied", err)
	}
}

func TestParseCallbackParams_StateMismatch(t *testing.T) {
	q := url.Values{"state": {"wrong"}, "code": {"the-code"}}
	_, err := parseCallbackParams(q, "abc")
	if !errors.Is(err, ErrInvalidCallback) {
		t.Fatalf("err = %v, want ErrInvalidCallback", err)
	}
}

func TestParseCallbackParams_MissingState(t *testing.T) {
	q := url.Values{"code": {"the-code"}}
	_, err := parseCallbackParams(q, "abc")
	if !errors.Is(err, ErrInvalidCallback) {
		t.Fatalf("err = %v, want ErrInvalidCallback", err)
	}
}

func TestParseCallbackParams_MissingCode(t *testing.T) {
	q := url.Values{"state": {"abc"}}
	_, err := parseCallbackParams(q, "abc")
	if !errors.Is(err, ErrInvalidCallback) {
		t.Fatalf("err = %v, want ErrInvalidCallback", err)
	}
}

func TestBuildAuthorizeURL(t *testing.T) {
	pkce := AuthCodePKCE{Verifier: "v", Challenge: "chal", State: "st"}
	got := BuildAuthorizeURL("client-123", pkce)

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("BuildAuthorizeURL produced unparseable URL: %v", err)
	}
	q := u.Query()
	for key, want := range map[string]string{
		"client_id":             "client-123",
		"state":                 "st",
		"code_challenge":        "chal",
		"code_challenge_method": "S256",
		"response_type":         "code",
		"redirect_uri":          oauthRedirectURI,
	} {
		if got := q.Get(key); got != want {
			t.Errorf("query param %q = %q, want %q", key, got, want)
		}
	}
}

func TestOAuthTokenFailuresDoNotExposeResponseBody(t *testing.T) {
	const secret = "oauth-response-secret-must-not-leak"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","refresh_token":"` + secret + `"}`))
	}))
	defer srv.Close()

	origURL := oauthTokenURL
	oauthTokenURL = srv.URL
	defer func() { oauthTokenURL = origURL }()

	tests := []struct {
		name string
		call func() error
		want string
	}{
		{
			name: "exchange",
			call: func() error {
				_, err := ExchangeCode(context.Background(), "client", "verifier", "code")
				return err
			},
			want: "token exchange failed with status 400",
		},
		{
			name: "refresh",
			call: func() error {
				_, err := RefreshToken(context.Background(), "client", "refresh")
				return err
			},
			want: "token refresh failed with status 400",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if err == nil {
				t.Fatal("expected error")
			}
			if got := err.Error(); got != tt.want {
				t.Errorf("error = %q, want %q", got, tt.want)
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error exposes response secret: %q", err)
			}
		})
	}
}

func TestRefreshToken_TransportAndResponseReadFailuresAreSafe(t *testing.T) {
	const secret = "refresh-path-secret-must-not-leak"
	originalTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = originalTransport }()

	tests := []struct {
		name      string
		transport http.RoundTripper
	}{
		{
			name: "transport",
			transport: oauthRoundTripper(func(*http.Request) (*http.Response, error) {
				return nil, errors.New(secret)
			}),
		},
		{
			name: "response read",
			transport: oauthRoundTripper(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       failingReadCloser{err: errors.New(secret)},
					Header:     make(http.Header),
				}, nil
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			http.DefaultTransport = tt.transport
			_, err := RefreshToken(context.Background(), "client", "refresh")
			if err == nil {
				t.Fatal("RefreshToken: expected error")
			}
			if got, want := err.Error(), "OAuth token refresh failed"; got != want {
				t.Errorf("error = %q, want %q", got, want)
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error exposes refresh failure detail: %q", err)
			}
		})
	}
}
