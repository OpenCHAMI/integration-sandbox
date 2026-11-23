package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(os.Stdout, "Usage: declarative-fixture <serve|fault-proxy> [flags]")
		return nil
	}
	mode := args[0]
	if mode != "serve" && mode != "fault-proxy" {
		return errors.New("unknown command; use --help")
	}
	flags := flag.NewFlagSet(mode, flag.ContinueOnError)
	listen := flags.String("listen", ":27780", "HTTP listen address")
	defaultUpstream := "http://tokensmith-backend:27780"
	if mode == "fault-proxy" {
		defaultUpstream = "http://boot:27790"
	}
	upstreamValue := flags.String("upstream", defaultUpstream, "upstream HTTP origin")
	issuer := flags.String("issuer", "http://tokensmith:27780", "fixture JWT issuer")
	keysDir := flags.String("keys-dir", "/fixture-keys", "run-only private key directory")
	if err := flags.Parse(args[1:]); errors.Is(err, flag.ErrHelp) {
		return nil
	} else if err != nil {
		return errors.New("invalid flags")
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	upstream, err := url.Parse(*upstreamValue)
	if err != nil || upstream == nil || (upstream.Scheme != "http" && upstream.Scheme != "https") || upstream.Host == "" || upstream.User != nil || upstream.RawQuery != "" || upstream.Fragment != "" || (upstream.Path != "" && upstream.Path != "/") {
		return errors.New("invalid upstream origin")
	}
	var handler http.Handler
	if mode == "serve" {
		issuerURL, err := url.Parse(*issuer)
		if err != nil || issuerURL.Host == "" || (issuerURL.Scheme != "http" && issuerURL.Scheme != "https") || issuerURL.User != nil || issuerURL.RawQuery != "" || issuerURL.Fragment != "" {
			return errors.New("invalid issuer")
		}
		handler, err = newFacade(upstream, *issuer, *keysDir)
		if err != nil {
			return errors.New("fixture key initialization failed")
		}
	} else {
		handler = newFaultProxy(upstream)
	}
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			server.Close()
		}
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen failed: %w", err)
	}
	return nil
}
