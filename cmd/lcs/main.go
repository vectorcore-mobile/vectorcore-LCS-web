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

	clients, err := buildGMLCClients(cfg.GMLCClients)
	if err != nil {
		fmt.Fprintf(os.Stderr, "VectorCore LCS: %v\n", err)
		os.Exit(1)
	}
	// clients["emergency"] is a nil gmlc.LocationClient (missing map key
	// returns the zero value) if no "emergency" profile is configured —
	// api.NewServer treats that as "no emergency routes", not an error.
	handler := api.NewServer(clients["default"], clients["emergency"], logger)

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

	logger.Info("starting VectorCore LCS", "addr", cfg.Addr(), "gmlc_clients", len(cfg.GMLCClients))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server error", "err", err)
		os.Exit(1)
	}
}

// buildGMLCClients constructs one LocationClient per configured profile,
// keyed by profile name — config.Load has already validated that a
// "default" profile exists and every profile's protocol is "json" or
// "mlp". Profiles beyond "default" (e.g. "emergency" — see L3 in
// docs/mlp-le-interface-plan.md) aren't wired to any route yet; building
// them here regardless keeps this factory the single place that knows how
// to turn a profile into a client, ready for internal/api to grow a
// second handler bound to one.
func buildGMLCClients(profiles []config.GMLCClient) (map[string]gmlc.LocationClient, error) {
	out := make(map[string]gmlc.LocationClient, len(profiles))
	for _, p := range profiles {
		switch p.Protocol {
		case "mlp":
			out[p.Name] = gmlc.NewMLP(p.BaseURL, p.ClientID, p.BearerToken, p.RequestTimeout)
		case "json":
			out[p.Name] = gmlc.New(p.BaseURL, p.ClientID, p.BearerToken, p.RequestTimeout)
		default:
			return nil, fmt.Errorf("gmlc_clients[%s]: unknown protocol %q", p.Name, p.Protocol)
		}
	}
	return out, nil
}
