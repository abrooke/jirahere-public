package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"strings"
	"syscall"
	"testing"

	"github.com/aslanbrooke/jirahere/internal/jira"
)

func netTestErr(msg string) error {
	return &net.OpError{Op: "dial", Net: "tcp", Err: errors.New(msg)}
}

type timeoutWrapErr struct{ err error }

func (e timeoutWrapErr) Error() string   { return "timeout" }
func (e timeoutWrapErr) Timeout() bool   { return true }
func (e timeoutWrapErr) Temporary() bool { return true }
func (e timeoutWrapErr) Unwrap() error   { return e.err }

func TestRenderJiraError_Classes(t *testing.T) {
	const (
		reach      = "could not reach Jira"
		unexpected = "Jira call failed unexpectedly"
	)
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"status 404", &jira.StatusError{StatusCode: 404, Body: "x"}, "Jira returned an unexpected response (404)"},
		{"status wrapped", fmt.Errorf("wrap: %w", &jira.StatusError{StatusCode: 500}), "Jira returned an unexpected response (500)"},
		{"status invalid code, no transport", &jira.StatusError{StatusCode: 0}, unexpected},
		{"net.OpError", netTestErr("refused"), reach},
		{"wrapped net error", fmt.Errorf("search: %w", netTestErr("refused")), reach},
		{"url.Error", &url.Error{Op: "Get", URL: "https://x", Err: errors.New("boom")}, reach},
		{"dns", &net.DNSError{Err: "no such host", Name: "x"}, reach},
		{"tls record header", tls.RecordHeaderError{Msg: "m"}, reach},
		{"tls alert", tls.AlertError(42), reach},
		{"x509 unknown authority", x509.UnknownAuthorityError{}, reach},
		{"x509 hostname", x509.HostnameError{Host: "x"}, reach},
		{"x509 invalid", x509.CertificateInvalidError{Reason: x509.Expired}, reach},
		{"errno", fmt.Errorf("w: %w", syscall.ECONNREFUSED), reach},
		{"deadline", context.DeadlineExceeded, reach},
		{"canceled wrapped", fmt.Errorf("w: %w", context.Canceled), reach},
		{"decode error", errors.New("invalid character '<' looking for beginning of value"), unexpected},
		{"request build", fmt.Errorf("build request: %w", errors.New("bad")), unexpected},
		{"nil-ish plain", errors.New("boom"), unexpected},
		{"local file ENOENT", fmt.Errorf("w: %w", &fs.PathError{Op: "open", Path: "x", Err: syscall.ENOENT}), unexpected},
		{"local file EACCES", fmt.Errorf("w: %w", &fs.PathError{Op: "open", Path: "x", Err: syscall.EACCES}), unexpected},
		{"local file ENOSPC", fmt.Errorf("w: %w", &fs.PathError{Op: "write", Path: "x", Err: syscall.ENOSPC}), unexpected},
		{"syscall ETIMEDOUT", fmt.Errorf("w: %w", syscall.ETIMEDOUT), reach},
		{"syscall EHOSTUNREACH", fmt.Errorf("w: %w", syscall.EHOSTUNREACH), reach},
		{"url.Error parse", &url.Error{Op: "parse", URL: "x", Err: errors.New("bad")}, unexpected},
		{"url.Error parse wrapped", fmt.Errorf("build: %w", &url.Error{Op: "parse", URL: "x", Err: errors.New("bad")}), unexpected},
		{"url.Error wrapping net error", &url.Error{Op: "Get", URL: "x", Err: netTestErr("refused")}, reach},
		{"unexpected EOF mid-body", fmt.Errorf("read body: %w", io.ErrUnexpectedEOF), reach},
		{"bare EOF", io.EOF, unexpected},
		{"OpError ENETDOWN", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ENETDOWN}, reach},
		{"OpError ENETRESET", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ENETRESET}, reach},
		{"OpError EHOSTDOWN", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.EHOSTDOWN}, reach},
		{"OpError EADDRNOTAVAIL", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.EADDRNOTAVAIL}, reach},

		{"OpError EMFILE", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.EMFILE}, unexpected},

		{"timeout net.Error wrapping PathError ENOENT", timeoutWrapErr{&fs.PathError{Op: "open", Path: "x", Err: syscall.ENOENT}}, unexpected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderJiraError(tt.err); got != tt.want {
				t.Errorf("renderJiraError = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderJiraError_NoLeak(t *testing.T) {
	const secret = "s3cr3t-token-XYZ"
	errs := []error{
		errors.New(secret),
		netTestErr(secret),
		&url.Error{Op: "Get", URL: "https://x/?t=" + secret, Err: errors.New(secret)},
		&jira.StatusError{StatusCode: 401, Body: secret},
		&jira.StatusError{StatusCode: 9999, Body: secret},
		fmt.Errorf("ctx %s: %w", secret, context.DeadlineExceeded),
	}
	for i, err := range errs {
		if got := renderJiraError(err); strings.Contains(got, secret) {
			t.Errorf("case %d: output %q leaks secret", i, got)
		}
	}
}
