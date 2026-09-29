package auth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"time"
)

var ErrNotLoggedIn = errors.New("not logged in")

func LoadProvider(prof string) (Provider, error) {
	cfg, err := Load(prof)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotLoggedIn
		}
		return nil, err
	}

	if err := ValidateSiteHostname(cfg.Site); err != nil {
		return nil, err
	}

	switch cfg.Provider {
	case ProviderAPIToken:
		if cfg.APIToken == nil {
			return nil, fmt.Errorf("config.json has provider %q but no api_token section", cfg.Provider)
		}
		return &apiTokenProvider{
			site:  cfg.Site,
			email: cfg.APIToken.Email,
			token: cfg.APIToken.Token,
		}, nil
	case ProviderOAuth:
		if cfg.OAuth == nil {
			return nil, fmt.Errorf("config.json has provider %q but no oauth section", cfg.Provider)
		}
		return &oauthProvider{cfg: cfg, profile: prof}, nil
	default:
		return nil, fmt.Errorf("config.json has unknown provider %q", cfg.Provider)
	}
}

type apiTokenProvider struct {
	site  string
	email string
	token string
}

func (p *apiTokenProvider) Credentials(ctx context.Context) (Credentials, error) {
	return Credentials{
		AuthHeader: BasicAuthHeader(p.email, p.token),
		BaseURL:    "https://" + p.site,
	}, nil
}

var cloudIDPattern = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

func validateCloudID(cloudID string) error {
	if cloudID == "" || !cloudIDPattern.MatchString(cloudID) {
		return fmt.Errorf("%q is not a valid Jira cloud ID", cloudID)
	}
	return nil
}

func oauthGatewayBaseURL(cloudID string) (string, error) {
	if err := validateCloudID(cloudID); err != nil {
		return "", err
	}
	return "https://api.atlassian.com/ex/jira/" + url.PathEscape(cloudID), nil
}

type oauthProvider struct {
	cfg *Config

	profile string
}

func (p *oauthProvider) Credentials(ctx context.Context) (Credentials, error) {
	if !time.Now().Before(p.cfg.OAuth.ExpiresAt) {
		if err := p.refresh(ctx); err != nil {
			return Credentials{}, err
		}
	}
	baseURL, err := oauthGatewayBaseURL(p.cfg.CloudID)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{
		AuthHeader: "Bearer " + p.cfg.OAuth.AccessToken,
		BaseURL:    baseURL,
	}, nil
}

func (p *oauthProvider) refresh(ctx context.Context) error {
	if OAuthClientID == "" {
		return errors.New("cannot refresh OAuth token: no client ID configured for this build")
	}
	tok, err := RefreshToken(ctx, OAuthClientID, p.cfg.OAuth.RefreshToken)
	if err != nil {

		return err
	}
	p.cfg.OAuth.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		p.cfg.OAuth.RefreshToken = tok.RefreshToken
	}
	p.cfg.OAuth.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	return Save(p.profile, p.cfg)
}
