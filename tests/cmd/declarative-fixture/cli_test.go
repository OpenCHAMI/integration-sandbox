package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCLIProcessesServeOnRealWire(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "fixture")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build command: %v: %s", err, output)
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/jwks.json" {
			ioWriteJSON(w, `{"keys":[{}]}`)
			return
		}
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	for _, mode := range []string{"serve", "fault-proxy"} {
		t.Run(mode, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := listener.Addr().String()
			if err := listener.Close(); err != nil {
				t.Fatal(err)
			}
			args := []string{mode, "--listen", address, "--upstream", backend.URL}
			if mode == "serve" {
				args = append(args, "--issuer", "http://tokensmith:27780", "--keys-dir", t.TempDir())
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			command := exec.CommandContext(ctx, binary, args...)
			if err := command.Start(); err != nil {
				cancel()
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if command.Process.Signal(os.Interrupt) != nil {
					cancel()
				}
				command.Wait()
				cancel()
			})
			client := &http.Client{Timeout: time.Second}
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			base := "http://" + address
			for {
				response, err := client.Get(base + "/__fixtures/health")
				if err == nil {
					response.Body.Close()
					if response.StatusCode == http.StatusOK {
						break
					}
				}
				select {
				case <-ctx.Done():
					t.Fatal("CLI did not become healthy")
				case <-ticker.C:
				}
			}
			if mode == "serve" {
				response, err := client.Get(base + "/.well-known/jwks.json")
				if err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				if response.StatusCode != http.StatusBadGateway {
					t.Fatalf("malformed upstream JWKS status = %d", response.StatusCode)
				}
			}
			response, err := client.Post(base+"/resource", "text/plain", strings.NewReader(""))
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			want := http.StatusCreated
			if mode == "fault-proxy" {
				want = http.StatusBadGateway
			}
			if response.StatusCode != want {
				t.Fatalf("wire status = %d, want %d", response.StatusCode, want)
			}
		})
	}
}
