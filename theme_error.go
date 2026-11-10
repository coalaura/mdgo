package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/CloudyKit/jet/v6"
)

type themeErrorWriter struct {
	http.ResponseWriter
	status int
}

var defaultErrorTemplate = sync.OnceValues(loadDefaultErrorTemplate)

func (writer *themeErrorWriter) WriteHeader(status int) {
	if writer.status != 0 {
		return
	}

	if status >= 200 {
		writer.status = status
	}

	if status < 400 {
		writer.ResponseWriter.WriteHeader(status)
	}
}

func (writer *themeErrorWriter) Write(contents []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}

	if writer.status >= 400 {
		return len(contents), nil
	}

	return writer.ResponseWriter.Write(contents)
}

func (writer *themeErrorWriter) ReadFrom(reader io.Reader) (int64, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}

	if writer.status >= 400 {
		return io.Copy(io.Discard, reader)
	}

	return io.Copy(writer.ResponseWriter, reader)
}

func (writer *themeErrorWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func (writer *themeErrorWriter) FlushError() error {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}

	if writer.status >= 400 {
		return nil
	}

	return http.NewResponseController(writer.ResponseWriter).Flush()
}

func (t *Theme) RenderError(status int, pagePath string) (contents []byte, err error) {
	defer func() {
		failure := recover()
		if failure != nil {
			contents = nil
			err = fmt.Errorf("error page rendering panicked: %v", failure)
		}
	}()

	data := map[string]any{
		"status":  status,
		"message": http.StatusText(status),
		"path":    pagePath,
	}

	if t != nil && t.errorLayout != nil {
		completed, err := renderErrorTemplate(t.errorLayout, t.catalogue, data, pagePath)
		if err == nil {
			return completed, nil
		}

		log.Warnln(fmt.Errorf("render theme error.html: %w", err))
	}

	fallback, err := defaultErrorTemplate()
	if err == nil {
		contents, err = renderErrorTemplate(fallback, nil, data, pagePath)
	}

	if err != nil {
		return nil, fmt.Errorf("render default error.html: %w", err)
	}

	return contents, nil
}

func ThemeErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Admin responses belong to the embedded UI, including login errors.
		if r.URL.Path == "/-" || strings.HasPrefix(r.URL.Path, "/-/") {
			next.ServeHTTP(w, r)

			return
		}

		writer := &themeErrorWriter{ResponseWriter: w}

		next.ServeHTTP(writer, r)

		if writer.status < 400 {
			return
		}

		writeThemeError(w, r, writer.status)
	})
}

func writeThemeError(w http.ResponseWriter, r *http.Request, status int) {
	siteMutex.RLock()
	contents, err := theme.RenderError(status, r.URL.EscapedPath())
	siteMutex.RUnlock()

	header := w.Header()

	header.Del("Content-Encoding")
	header.Del("Content-Length")
	header.Del("ETag")
	header.Del("Last-Modified")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")

	header.Set("Content-Type", "text/html; charset=utf-8")

	if err != nil {
		log.Warnln(err)
		message := http.StatusText(status)

		if message == "" {
			message = "Request failed"
		}

		contents = []byte(message + "\n")

		header.Set("Content-Type", "text/plain; charset=utf-8")
	}

	header.Set("Content-Length", strconv.Itoa(len(contents)))

	w.WriteHeader(status)

	if r.Method != http.MethodHead {
		w.Write(contents)
	}
}

func loadDefaultErrorTemplate() (*jet.Template, error) {
	contents, err := ThemeDefaultFS.ReadFile("themes/default/error.html")
	if err != nil {
		return nil, err
	}

	loader := jet.NewInMemLoader()
	loader.Set("error.html", string(contents))

	templates := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{""}))

	return templates.GetTemplate("error.html")
}

func renderErrorTemplate(layout *jet.Template, catalogue *AssetCatalogue, data map[string]any, pagePath string) (contents []byte, err error) {
	defer func() {
		failure := recover()
		if failure != nil {
			contents = nil
			err = fmt.Errorf("error template panicked: %v", failure)
		}
	}()

	var rendered bytes.Buffer

	err = layout.Execute(&rendered, nil, data)
	if err != nil {
		return nil, err
	}

	if len(bytes.TrimSpace(rendered.Bytes())) == 0 {
		return nil, errors.New("error template produced an empty page")
	}

	if catalogue != nil {
		return catalogue.RewriteHTML(rendered.Bytes(), pagePath)
	}

	return rendered.Bytes(), nil
}
