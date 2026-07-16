package server

import (
	"net/http"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/config"
)

func newHTTPServer(cfg *config.Server, rt *runtime) *http.Server {
	return &http.Server{
		Addr:              cfg.HTTP.Listen,
		Handler:           rt.handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
}
