package main

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"
)

func main() {
	ctx := context.Background()
	if err := run(ctx, os.Getenv); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

type Config struct {
	Port          string
	DownstreamURL string
}

func run(
	ctx context.Context,
	getenv func(string) string,
) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer func() {
		log.Println("cancelling run's notify context")
		cancel()
	}()

	config := createConfig(getenv)

	srv := NewServer(config)
	httpServer := &http.Server{
		Addr:    net.JoinHostPort("localhost", config.Port),
		Handler: srv,
	}
	go func() {
		log.Printf("listening on %s", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "error listening and serving: %s\n", err)
		}
	}()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		// Make a new context for the Shutdown (thanks Alessandro Rosetti)
		shutdownCtx := context.Background()
		shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "error shutting down http server: %s\n", err)
		} else {
			fmt.Fprintf(os.Stdout, "http server shutdown\n")
		}
	}()
	wg.Wait()
	return nil
}

func NewServer(
	config Config,
) http.Handler {
	mux := http.NewServeMux()
	addRoutes(mux, config)
	var handler http.Handler = mux
	return handler
}

func addRoutes(
	mux *http.ServeMux,
	config Config,
) {
	mux.Handle("/lookup", handleLookup(config.DownstreamURL))
}

func handleLookup(downstreamURL string) http.Handler {
	if downstreamURL == "" {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("hey!! lookup"))
		})
	} else {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Define the URL of the external server
			externalURL := downstreamURL + r.URL.Path

			// Create a new request to the external server
			req, err := http.NewRequest("GET", externalURL, nil)
			if err != nil {
				http.Error(w, "Failed to create request", http.StatusInternalServerError)
				return
			}

			// Optionally, you can copy headers from the original request to the new request
			for key, values := range r.Header {
				for _, value := range values {
					req.Header.Add(key, value)
				}
			}

			// Create an HTTP client
			client := &http.Client{
				Timeout: 10 * time.Second,
			}

			// Make the request to the external server
			resp, err := client.Do(req)
			if err != nil {
				http.Error(w, "Failed to get response from external server", http.StatusInternalServerError)
				return
			}
			defer resp.Body.Close()

			// Read the response body
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				http.Error(w, "Failed to read response body", http.StatusInternalServerError)
				return
			}

			// Write the response from the external server back to the client
			for key, values := range resp.Header {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
			w.WriteHeader(resp.StatusCode)
			w.Write(body)
		})
	}
}

func createConfig(getenv func(string) string) Config {
	port := getenv("PORT")
	downstreamURL := getenv("DOWNSTREAM_URL")
	return Config{
		Port:          port,
		DownstreamURL: downstreamURL,
	}
}
