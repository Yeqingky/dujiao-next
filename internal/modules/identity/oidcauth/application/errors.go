package oidcauthapp

import "errors"

var (
	ErrOIDCAuthDisabled      = errors.New("oidc auth disabled")
	ErrOIDCAuthConfigInvalid = errors.New("oidc auth config invalid")
	ErrOIDCStateInvalid      = errors.New("oidc state invalid")
	ErrOIDCTokenExchange     = errors.New("oidc token exchange failed")
	ErrOIDCIDTokenInvalid    = errors.New("oidc id token invalid")
	ErrOIDCPayloadInvalid    = errors.New("oidc payload invalid")
	ErrOIDCDiscoveryFailed   = errors.New("oidc discovery failed")
	ErrOIDCJWKSUnavailable   = errors.New("oidc jwks unavailable")
)
