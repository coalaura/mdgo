package main

import (
	"bufio"
	"crypto/rand"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/coalaura/lock"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
)

const PublicDirectory = "public"

var (
	frontendRenderLocks = lock.NewLockMap[string]()
	frontendParser      = parser.New()
	frontendRenderer    = html.New()
)

func LoadPublicRoot() (*os.Root, error) {
	_, err := os.Stat(PublicDirectory)
	if os.IsNotExist(err) {
		os.MkdirAll(PublicDirectory, 0755)
	}

	return os.OpenRoot(PublicDirectory)
}

func HandleFrontend(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" || strings.HasSuffix(name, "/") {
		name += "index"
	}

	if !fs.ValidPath(name) || strings.ContainsAny(name, "\\:") {
		http.NotFound(w, r)

		return
	}

	err := serveFrontendFile(w, r, name)
	if !errors.Is(err, fs.ErrNotExist) {
		writeFrontendError(w, r, err)

		return
	}

	if !strings.HasSuffix(name, ".html") {
		name += ".html"

		err = serveFrontendFile(w, r, name)
		if !errors.Is(err, fs.ErrNotExist) {
			writeFrontendError(w, r, err)

			return
		}
	}

	err = renderFrontendFile(name)
	if err != nil {
		writeFrontendError(w, r, err)

		return
	}

	err = serveFrontendFile(w, r, name)

	writeFrontendError(w, r, err)
}

func serveFrontendFile(w http.ResponseWriter, r *http.Request, name string) error {
	file, err := public.Open(name)
	if err != nil {
		return err
	}

	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	if !info.Mode().IsRegular() {
		return fs.ErrNotExist
	}

	http.ServeContent(w, r, name, info.ModTime(), file)

	return nil
}

func renderFrontendFile(name string) error {
	frontendRenderLocks.Lock(name)
	defer frontendRenderLocks.Unlock(name)

	_, err := public.Stat(name)
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	source, err := public.ReadFile(strings.TrimSuffix(name, ".html") + ".md")
	if err != nil {
		return err
	}

	temporaryName := path.Join(path.Dir(name), ".mdgo-"+rand.Text()+".tmp")

	file, err := public.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}

	defer public.Remove(temporaryName)
	defer file.Close()

	writer := bufio.NewWriter(file)
	document := frontendParser.Parse(source)

	err = frontendRenderer.Render(writer, source, document)
	if err != nil {
		return err
	}

	err = writer.Flush()
	if err != nil {
		return err
	}

	err = file.Close()
	if err != nil {
		return err
	}

	return public.Rename(temporaryName, name)
}

func writeFrontendError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}

	if errors.Is(err, fs.ErrNotExist) {
		http.NotFound(w, r)

		return
	}

	if errors.Is(err, fs.ErrPermission) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)

		return
	}

	log.Warnln(err)

	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}
