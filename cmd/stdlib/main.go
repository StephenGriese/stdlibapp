package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Create a context that is cancellable
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Channel to listen for errors from HTTP server
	serverErrChan := make(chan error, 1)

	// Start HTTP server
	srv := &http.Server{Addr: ":8080"}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello, world!")
	})

	go func() {
		log.Println("Starting HTTP server on :8080")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrChan <- err
		}
	}()

	// Start a goroutine to read from stdin
	go func(ctx context.Context) {
		reader := bufio.NewReader(os.Stdin)
		for {
			select {
			case <-ctx.Done():
				log.Println("Shutting down stdin reader")
				return
			default:
				fmt.Print("Enter text: ")
				text, _ := reader.ReadString('\n')
				log.Printf("You entered: %s", text)
			}
		}
	}(ctx)

	// Listen for interrupt signals to gracefully shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case <-stop:
		log.Println("Received interrupt signal, shutting down...")
		cancel()
	case err := <-serverErrChan:
		log.Printf("HTTP server error: %v", err)
		cancel()
	}

	// Create a context with timeout for the server shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("HTTP server shutdown failed: %v", err)
	}

	log.Println("Server gracefully stopped")
}
