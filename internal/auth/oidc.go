package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDC wraps an OpenID Connect provider (Authentik) using the code flow with PKCE.
type OIDC struct {
	provider      *oidc.Provider
	oauth         oauth2.Config
	verifier      *oidc.IDTokenVerifier
	endSession    string
	allowedGroups []string
}

// Claims we use from the ID token.
type Claims struct {
	Sub               string   `json:"sub"`
	Email             string   `json:"email"`
	Name              string   `json:"name"`
	PreferredUsername string   `json:"preferred_username"`
	Groups            []string `json:"groups"`
	Nonce             string   `json:"nonce"`
}

// NewOIDC discovers the provider and builds the client. baseURL is the public URL of this app.
func NewOIDC(ctx context.Context, issuer, clientID, clientSecret, baseURL string, allowedGroups []string) (*OIDC, error) {
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	var meta struct {
		EndSession string `json:"end_session_endpoint"`
	}
	_ = p.Claims(&meta)
	return &OIDC{
		provider: p,
		oauth: oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     p.Endpoint(),
			RedirectURL:  strings.TrimRight(baseURL, "/") + "/auth/callback",
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		verifier:      p.Verifier(&oidc.Config{ClientID: clientID}),
		endSession:    meta.EndSession,
		allowedGroups: allowedGroups,
	}, nil
}

// SetScopes overrides the requested scopes (openid is always included).
func (o *OIDC) SetScopes(scopes []string) {
	out := []string{oidc.ScopeOpenID}
	for _, s := range scopes {
		if s != "" && s != oidc.ScopeOpenID {
			out = append(out, s)
		}
	}
	o.oauth.Scopes = out
}

// AuthURL builds the authorization redirect.
func (o *OIDC) AuthURL(state, nonce, verifier string) string {
	return o.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
}

// Exchange swaps the code for tokens, verifies the ID token and nonce, and checks groups.
func (o *OIDC) Exchange(ctx context.Context, code, nonce, verifier string) (*Claims, string, error) {
	tok, err := o.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, "", err
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return nil, "", errors.New("no id_token in response")
	}
	idt, err := o.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, "", err
	}
	var c Claims
	if err := idt.Claims(&c); err != nil {
		return nil, "", err
	}
	if c.Nonce != nonce {
		return nil, "", errors.New("nonce mismatch")
	}
	if !o.groupAllowed(c.Groups) {
		return nil, "", errors.New("user is not in an allowed group")
	}
	if c.Name == "" {
		c.Name = c.PreferredUsername
	}
	return &c, raw, nil
}

func (o *OIDC) groupAllowed(have []string) bool {
	if len(o.allowedGroups) == 0 {
		return true
	}
	for _, h := range have {
		for _, a := range o.allowedGroups {
			if h == a {
				return true
			}
		}
	}
	return false
}

// LogoutURL returns the provider's end-session URL, or "" if unsupported.
func (o *OIDC) LogoutURL(idToken, postLogoutRedirect string) string {
	if o.endSession == "" {
		return ""
	}
	u := o.endSession + "?post_logout_redirect_uri=" + urlQueryEscape(postLogoutRedirect)
	if idToken != "" {
		u += "&id_token_hint=" + urlQueryEscape(idToken)
	}
	return u
}
