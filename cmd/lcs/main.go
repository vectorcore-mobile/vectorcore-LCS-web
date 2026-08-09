// Command lcs runs the VectorCore LCS console: a web UI + thin proxy API
// that requests UE locations from a GMLC over its Le REST/JSON adapter.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vectorcore/lcs/internal/api"
	"github.com/vectorcore/lcs/internal/config"
	"github.com/vectorcore/lcs/internal/gmlc"
)

const appVersion = "0.1.0"

func main() {
	var cfgPath string
	var showVer, debugMode bool

	flag.StringVar(&cfgPath, "c", "config/lcs.yaml", "path to config file")
	flag.BoolVar(&showVer, "version", false, "print version and exit")
	flag.BoolVar(&showVer, "v", false, "print version and exit (shorthand)")
	flag.BoolVar(&debugMode, "d", false, "also log to stdout regardless of config")
	flag.Parse()

	if showVer {
		fmt.Printf("VectorCore LCS v%s\n", appVersion)
		os.Exit(0)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "VectorCore LCS: %v\n", err)
		os.Exit(1)
	}

	logger, closeLog := buildLogger(cfg.Log, debugMode)
	defer closeLog()

	client := gmlc.New(cfg.GMLC.BaseURL, cfg.GMLC.ClientID, cfg.GMLC.BearerToken, cfg.GMLC.RequestTimeout)
	handler := api.NewServer(client, logger)

	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: handler,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	logger.Info("starting VectorCore LCS", "addr", cfg.Addr(), "gmlc_base_url", cfg.GMLC.BaseURL)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server error", "err", err)
		os.Exit(1)
	}
}
