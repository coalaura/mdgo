package main

import (
	"context"
	"net/http"
	"net/url"
)

type contextKey string

const authKey contextKey = "auth"

func IsAuthenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var authenticated bool

		cookie, err := r.Cookie("auth_token")
		if err == nil {
			authenticated = VerifyAuthToken(cookie.Value)
		}

		ctx := context.WithValue(r.Context(), authKey, authenticated)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequiresAuthentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Value(authKey) != true {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			origin := r.Header.Get("Origin")
			if origin != "" {
				parsed, err := url.Parse(origin)
				if err != nil || parsed.Host != r.Host {
					w.WriteHeader(http.StatusForbidden)

					return
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}
