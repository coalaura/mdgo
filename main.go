package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/coalaura/plain"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

var (
	config *Config
	theme  *Theme
	public *os.Root

	log = plain.New(plain.WithDate(plain.RFC3339Local))
)

func main() {
	var err error

	log.Println("Loading config...")

	config, err = LoadConfig()
	log.MustFail(err)

	log.Println("Loading theme...")

	theme, err = LoadTheme()
	log.MustFail(err)

	log.Println("Loading root...")

	public, err = LoadPublicRoot()
	log.MustFail(err)

	log.Println("Preparing router...")

	r := chi.NewRouter()

	r.Use(middleware.Recoverer)
	r.Use(log.Middleware())

	r.Get("/*", HandleFrontend)
	r.Head("/*", HandleFrontend)

	r.Group(func(gr chi.Router) {
		gr.Use(IsAuthenticated)

		gr.Get("/-/login", HandleLogin)
		gr.Post("/-/login", HandleLogin)
		gr.Get("/-/logout", HandleLogout)
		gr.Get("/-/admin", HandleAdmin)
	})

	r.Group(func(gr chi.Router) {
		gr.Use(RequiresAuthentication)
	})

	addr := config.Addr()

	server := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := context.WithCancelCause(context.Background())

	go func() {
		log.Printf("Listening at http://localhost%s/\n", addr)

		err = server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			cancel(err)
		}
	}()

	log.WaitForInterruptCtx(ctx)

	err = context.Cause(ctx)
	if err != nil {
		log.Errorln(err)

		os.Exit(1)
	}

	log.Warnln("Shutting down...")

	server.Close()
}
