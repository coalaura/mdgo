package main

import (
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

const adminFormLimit = 8192

func HandleAdmin(w http.ResponseWriter, r *http.Request) {
	notice := r.URL.Query().Get("notice")
	message := ""

	switch notice {
	case "page-cleared":
		message = "Page cache cleared."
	case "all-cleared":
		message = "Page caches cleared and assets rebuilt."
	}

	renderAdminDashboard(w, r, message)
}

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Context().Value(authKey) == true {
		redirectAdmin(w, r, "/-/admin")

		return
	}

	if r.Method == http.MethodPost {
		if !parseAdminForm(w, r) {
			return
		}

		limited := CheckRateLimit(r, RateLimitLogin)
		if limited != nil {
			seconds := max(1, int((*limited+time.Second-1)/time.Second))
			w.Header().Set("Retry-After", strconv.Itoa(seconds))

			renderAdminPage(w, r, http.StatusTooManyRequests, adminPageData{View: "login", Title: "Sign in", Error: "Too many attempts. Please wait a moment before trying again."})

			return
		}

		token := r.PostForm.Get("token")

		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(config.Tokens.Auth)) != 1 {
			renderAdminPage(w, r, http.StatusUnauthorized, adminPageData{View: "login", Title: "Sign in", Error: "That token was not recognized. Please try again."})

			return
		}

		session, err := MintAuthToken()
		if err != nil {
			log.Warnln(err)

			renderAdminPage(w, r, http.StatusInternalServerError, adminPageData{View: "login", Title: "Sign in", Error: "Sign in is temporarily unavailable. Please try again."})

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
			Secure:   config.Server.Secure,
		})

		redirectAdmin(w, r, "/-/admin")

		return
	}

	renderAdminPage(w, r, http.StatusOK, adminPageData{View: "login", Title: "Sign in"})
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
			Secure:   config.Server.Secure,
		})
	}

	redirectAdmin(w, r, "/-/login")
}

func HandleClearPageCache(w http.ResponseWriter, r *http.Request) {
	err := clearMarkdownPageCache(r.PostForm.Get("path"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrInvalid) {
			writeAdminError(w, r, http.StatusBadRequest, "Choose an existing Markdown page from the dashboard.")
		} else {
			log.Warnln(err)

			writeAdminError(w, r, http.StatusInternalServerError, "The page cache could not be cleared. Please try again.")
		}

		return
	}

	if wantsAdminJSON(r) {
		renderAdminDashboard(w, r, "Page cache cleared.")

		return
	}

	http.Redirect(w, r, "/-/admin?notice=page-cleared", http.StatusSeeOther)
}

func HandleClearAllCache(w http.ResponseWriter, r *http.Request) {
	err := ClearThemeCache()
	if err != nil {
		log.Warnln(err)

		writeAdminError(w, r, http.StatusInternalServerError, "The cache clear could not be completed. Check the server log for details.")

		return
	}

	if wantsAdminJSON(r) {
		renderAdminDashboard(w, r, "Page caches cleared and assets rebuilt.")

		return
	}

	http.Redirect(w, r, "/-/admin?notice=all-cleared", http.StatusSeeOther)
}

func renderAdminDashboard(w http.ResponseWriter, r *http.Request, message string) {
	pages, err := listMarkdownPages()
	if err != nil {
		log.Warnln(err)

		writeAdminError(w, r, http.StatusInternalServerError, "The Markdown file list could not be read. Please try again.")

		return
	}

	data := adminPageData{View: "dashboard", Title: "Dashboard", Pages: pages, Notice: message}

	for _, page := range pages {
		if page.Cached {
			data.Cached++
		} else {
			data.Uncached++
		}
	}

	renderAdminPage(w, r, http.StatusOK, data)
}

func RegisterAdminRoutes(router chi.Router) {
	protection := http.NewCrossOriginProtection()

	protection.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAdminError(w, r, http.StatusForbidden, "This request must be submitted from this site's admin panel.")
	}))

	router.Route("/-", func(admin chi.Router) {
		admin.Use(AdminResponseHeaders)
		admin.Use(IsAuthenticated)
		admin.Use(protection.Handler)

		admin.Get("/login", HandleLogin)
		admin.Head("/login", HandleLogin)
		admin.Post("/login", HandleLogin)
		admin.Get("/styles.css", serveAdminStyles)
		admin.Head("/styles.css", serveAdminStyles)
		admin.Get("/admin.js", serveAdminScript)
		admin.Head("/admin.js", serveAdminScript)

		admin.NotFound(func(w http.ResponseWriter, r *http.Request) {
			writeAdminError(w, r, http.StatusNotFound, "This admin page does not exist.")
		})

		admin.Group(func(protected chi.Router) {
			protected.Use(RequiresAuthentication)
			protected.Use(requireAdminForm)
			protected.Get("/", HandleAdmin)
			protected.Get("/admin", HandleAdmin)
			protected.Head("/admin", HandleAdmin)
			protected.Post("/logout", HandleLogout)
			protected.Post("/cache/page", HandleClearPageCache)
			protected.Post("/cache/all", HandleClearAllCache)
		})
	})
}

func AdminResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")

		next.ServeHTTP(w, r)
	})
}

func requireAdminForm(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if !parseAdminForm(w, r) {
				return
			}

			expected := adminCSRFToken(r)
			provided := r.PostForm.Get("csrf")

			if expected == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) != 1 {
				writeAdminError(w, r, http.StatusForbidden, "This form has expired. Return to the dashboard and try again.")

				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func parseAdminForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, adminFormLimit)

	err := r.ParseForm()
	if err != nil {
		writeAdminError(w, r, http.StatusBadRequest, "The submitted form could not be read. Please try again.")

		return false
	}

	return true
}

func adminCSRFToken(r *http.Request) string {
	cookie, err := r.Cookie("auth_token")
	if err != nil || r.Context().Value(authKey) != true {
		return ""
	}

	return hex.EncodeToString(AuthHMAC("admin-csrf:" + cookie.Value))
}
