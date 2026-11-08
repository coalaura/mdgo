package main

import (
	"crypto/subtle"
	"net/http"
)

type LoginRequest struct {
	Token string `json:"token"`
}

func HandleAdmin(w http.ResponseWriter, r *http.Request) {
	if r.Context().Value(authKey) != true {
		http.Redirect(w, r, "/-/login", http.StatusFound)

		return
	}

	// render admin page
}

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Context().Value(authKey) == true {
		http.Redirect(w, r, "/-/admin", http.StatusFound)

		return
	}

	if r.Method == http.MethodPost {
		limited := CheckRateLimit(r, RateLimitLogin)

		if limited != nil {
			// render login with error

			return
		}

		token := r.FormValue("token")

		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(config.Tokens.Auth)) != 1 {
			// render login with error

			return
		}

		session, err := MintAuthToken()
		if err != nil {
			// render login with error

			return
		}

		ClearRateLimit(r, RateLimitLogin)

		http.SetCookie(w, &http.Cookie{
			Name:     "auth_token",
			Value:    session,
			Path:     "/-/",
			HttpOnly: true,
			MaxAge:   int(AuthTokenTTL.Seconds()),
			SameSite: http.SameSiteStrictMode,
		})

		http.Redirect(w, r, "/-/admin", http.StatusFound)

		return
	}

	// render login page
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Context().Value(authKey) == true {
		http.SetCookie(w, &http.Cookie{
			Name:     "auth_token",
			Value:    "",
			Path:     "/-/",
			HttpOnly: true,
			MaxAge:   -1,
			SameSite: http.SameSiteStrictMode,
		})
	}

	http.Redirect(w, r, "/", http.StatusFound)
}
