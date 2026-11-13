package main

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
)

const (
	cacheRevalidate = "no-cache"
	cacheImmutable  = "public, max-age=31536000, immutable"
	cachePrivate    = "no-store"
)

type cacheResponseWriter struct {
	http.ResponseWriter
	private bool
	written bool
}

func (writer *cacheResponseWriter) WriteHeader(status int) {
	if writer.written {
		return
	}

	if status >= 200 {
		writer.written = true
	}

	header := writer.Header()

	if writer.private || status >= 400 || len(header.Values("Set-Cookie")) != 0 || privateCachePolicy(header.Get("Cache-Control")) {
		header.Set("Cache-Control", cachePrivate)
		header.Del("Expires")
	} else if header.Get("Cache-Control") == "" {
		header.Set("Cache-Control", cacheRevalidate)
	}

	if status >= 400 {
		header.Del("ETag")
		header.Del("Last-Modified")
	}

	writer.ResponseWriter.WriteHeader(status)
}

func (writer *cacheResponseWriter) Write(contents []byte) (int, error) {
	if !writer.written {
		writer.WriteHeader(http.StatusOK)
	}

	return writer.ResponseWriter.Write(contents)
}

func (writer *cacheResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
	if !writer.written {
		writer.WriteHeader(http.StatusOK)
	}

	return io.Copy(writer.ResponseWriter, reader)
}

func (writer *cacheResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func ResponseCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		private := r.URL.Path == "/-" || strings.HasPrefix(r.URL.Path, "/-/") || r.Context().Value(authKey) == true || r.Header.Get("Authorization") != ""

		cookie, err := r.Cookie("auth_token")
		if err == nil && VerifyAuthToken(cookie.Value) {
			private = true
		}

		policy := cacheRevalidate

		if private {
			policy = cachePrivate
		}

		w.Header().Set("Cache-Control", policy)

		writer := &cacheResponseWriter{ResponseWriter: w, private: private}

		next.ServeHTTP(writer, r)
	})
}

func setPublicCacheHeaders(w http.ResponseWriter, policy, etag string) {
	if !privateCachePolicy(w.Header().Get("Cache-Control")) {
		w.Header().Set("Cache-Control", policy)
	}

	if etag != "" {
		w.Header().Set("ETag", etag)
	}
}

func privateCachePolicy(policy string) bool {
	for directive := range strings.SplitSeq(policy, ",") {
		name, _, _ := strings.Cut(strings.TrimSpace(directive), "=")
		if strings.EqualFold(name, "private") || strings.EqualFold(name, "no-store") {
			return true
		}
	}

	return false
}

func contentETag(contents []byte) string {
	digest := sha256.Sum256(contents)

	return `"sha256-` + base64.StdEncoding.EncodeToString(digest[:]) + `"`
}
