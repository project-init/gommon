package auth

import (
	"context"

	"github.com/coreos/go-oidc"
	"golang.org/x/oauth2"
)

type OauthHandler struct {
	clientId     string
	clientSecret string
	redirectUrl  string
	issuerUrl    string
	oIDCProvider *oidc.Provider
	oAuth2Config oauth2.Config
}

// NewOauthHandler creates a handler that is meant to operate between oidc and oauth to handle the necessary redirects
// and issuer interactions. This has been tested against Cognito specifically.
func NewOauthHandler(clientId string, clientSecret string, redirectUrl string, issuerUrl string, scopes []string) (*OauthHandler, error) {
	provider, err := oidc.NewProvider(context.Background(), issuerUrl)
	if err != nil {
		return nil, err
	}

	return &OauthHandler{
		clientId:     clientId,
		clientSecret: clientSecret,
		redirectUrl:  redirectUrl,
		issuerUrl:    issuerUrl,
		oIDCProvider: provider,
		oAuth2Config: oauth2.Config{
			ClientID:     clientId,
			ClientSecret: clientSecret,
			RedirectURL:  redirectUrl,
			Endpoint:     provider.Endpoint(),
			Scopes:       scopes,
		},
	}, nil
}

func (h *OauthHandler) AuthCodeUrl(state string, opts ...oauth2.AuthCodeOption) string {
	return h.oAuth2Config.AuthCodeURL(state, opts...)
}

func (h *OauthHandler) ExchangeCode(ctx context.Context, code string) (*oauth2.Token, error) {
	return h.oAuth2Config.Exchange(ctx, code)
}

func (h *OauthHandler) Verifier() *oidc.IDTokenVerifier {
	return h.oIDCProvider.Verifier(&oidc.Config{ClientID: h.clientId})
}
