package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type adminPageData struct {
	View          string
	Title         string
	Error         string
	Notice        string
	Authenticated bool
	CSRF          string
	Pages         []adminMarkdownPage
	Cached        int
	Uncached      int
}

type adminResponse struct {
	Redirect  string          `json:"redirect,omitempty"`
	Error     string          `json:"error,omitempty"`
	Message   string          `json:"message,omitempty"`
	Dashboard *adminDashboard `json:"dashboard,omitempty"`
}

type adminDashboard struct {
	Pages    []adminMarkdownPage `json:"pages"`
	Cached   int                 `json:"cached"`
	Uncached int                 `json:"uncached"`
}

var (
	//go:embed admin/page.html
	adminPageSource string

	//go:embed admin/styles.css
	adminStyles string

	//go:embed admin/admin.js
	adminScript string

	adminTemplate = sync.OnceValues(loadAdminTemplate)
)

func renderAdminPage(w http.ResponseWriter, r *http.Request, status int, data adminPageData) {
	if wantsAdminJSON(r) {
		response := adminResponse{Error: data.Error, Message: data.Notice}

		if data.View == "dashboard" {
			response.Dashboard = &adminDashboard{Pages: data.Pages, Cached: data.Cached, Uncached: data.Uncached}
		}

		writeAdminJSON(w, r, status, response)

		return
	}

	data.Authenticated = r.Context().Value(authKey) == true
	data.CSRF = adminCSRFToken(r)

	var rendered bytes.Buffer

	layout, err := adminTemplate()
	if err == nil {
		err = layout.Execute(&rendered, data)
	}

	if err != nil {
		log.Warnln(err)

		http.Error(w, "The admin page could not be displayed.", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(rendered.Len()))
	w.WriteHeader(status)

	if r.Method != http.MethodHead {
		w.Write(rendered.Bytes())
	}
}

func writeAdminError(w http.ResponseWriter, r *http.Request, status int, message string) {
	renderAdminPage(w, r, status, adminPageData{View: "error", Title: http.StatusText(status), Error: message})
}

func serveAdminStyles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")

	http.ServeContent(w, r, "styles.css", time.Time{}, strings.NewReader(adminStyles))
}

func serveAdminScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")

	http.ServeContent(w, r, "admin.js", time.Time{}, strings.NewReader(adminScript))
}

func redirectAdmin(w http.ResponseWriter, r *http.Request, location string) {
	if wantsAdminJSON(r) {
		writeAdminJSON(w, r, http.StatusOK, adminResponse{Redirect: location})

		return
	}

	http.Redirect(w, r, location, http.StatusSeeOther)
}

func writeAdminJSON(w http.ResponseWriter, r *http.Request, status int, response adminResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if r.Method == http.MethodHead {
		return
	}

	err := json.NewEncoder(w).Encode(response)
	if err != nil {
		log.Warnln(err)
	}
}

func wantsAdminJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

func loadAdminTemplate() (*template.Template, error) {
	return template.New("admin").Parse(adminPageSource)
}
