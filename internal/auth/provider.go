package auth

import "context"

type Credentials struct {
	AuthHeader string
	BaseURL    string
}

type Provider interface {
	Credentials(ctx context.Context) (Credentials, error)
}
