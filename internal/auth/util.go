package auth

import "net/url"

func urlQueryEscape(s string) string { return url.QueryEscape(s) }
