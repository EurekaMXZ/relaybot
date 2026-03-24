package httpserver

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func New(addr string, webhookPath string, webhookHandler http.Handler, readyCheck func(context.Context) error) *http.Server {
	mux := http.NewServeMux()
	if webhookHandler != nil && webhookPath != "" {
		mux.Handle(webhookPath, webhookHandler)
	}
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if readyCheck != nil {
			if err := readyCheck(ctx); err != nil {
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})

	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func IsExpectedShutdown(err error) bool {
	return err == nil || errors.Is(err, http.ErrServerClosed)
}
