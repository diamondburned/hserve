package hserve

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"
)

// DefaultListenConfig is the default ListenConfig used for Listen().
var DefaultListenConfig = &net.ListenConfig{}

// ShutdownTimeout is the timeout to wait when shutting down HTTP.
var ShutdownTimeout = 10 * time.Second

// MustListenAndServe does ListenAndServe and log.Fatals on an error.
func MustListenAndServe(ctx context.Context, address string, handler http.Handler) {
	if err := ListenAndServe(ctx, address, handler); err != nil {
		log.Fatalln("cannot listen and serve HTTP:", err)
	}
}

// ListenAndServe listens and serves HTTP until a SIGINT is received.
func ListenAndServe(ctx context.Context, address string, handler http.Handler) error {
	return ListenAndServeExisting(ctx, &http.Server{Addr: address, Handler: handler})
}

// MustListenAndServeExisting does ListenAndServeExisting and log.Fatals on an
// error.
func MustListenAndServeExisting(ctx context.Context, server *http.Server) {
	if err := ListenAndServeExisting(ctx, server); err != nil {
		log.Fatalln("cannot listen and serve HTTP:", err)
	}
}

// ListenAndServeExisting listens to the address set in http.Server and serves
// HTTP. It gracefully exits if the given ctx expires.
func ListenAndServeExisting(ctx context.Context, server *http.Server) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	l, err := Listen(ctx, server.Addr)
	if err != nil {
		return err
	}
	defer l.Close()

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(l) }()

	select {
	case <-ctx.Done():
		// Context finish is not a fatal error.
	case serveErr := <-serveErr:
		if serveErr != http.ErrServerClosed {
			err = serveErr
		}
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("failed to shutdown gracefully: %w", err)
	}

	return err
}

// Listen calls ListenWithConfig with the DefaultListenConfig.
func Listen(ctx context.Context, address string) (net.Listener, error) {
	return ListenWithConfig(ctx, address, DefaultListenConfig)
}

// ListenWithConfig listens for incoming connections using the given address
// string. The address can be formatted as such:
//
//    127.0.0.1:29485 (implicitly tcp)
//    tcp://127.0.0.1:29485
//    tcp4://127.0.0.1:29485
//    unix:///tmp/path/to/socket.sock (will be automatically cleaned up)
//    unixpacket:///tmp/path/to/socket.sock (same as unix)
//
func ListenWithConfig(ctx context.Context, address string, config *net.ListenConfig) (net.Listener, error) {
	if !strings.Contains(address, "://") {
		address = "tcp://" + address
	}

	listenURL, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("failed to parse address: %w", err)
	}

	var (
		scheme = listenURL.Scheme
		addr   = listenURL.Host + listenURL.Path
	)

	// Do cleanup just in case.
	if err := cleanup(scheme, addr); err != nil {
		return nil, fmt.Errorf("failed to run cleanup prior to listening: %w", err)
	}

	l, err := config.Listen(ctx, scheme, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen at %v: %w", listenURL, err)
	}

	return listener{l, scheme, addr}, nil
}

// listener implements edge cases for Close().
type listener struct {
	net.Listener
	scheme string
	addr   string
}

func (l listener) Close() error {
	defer cleanup(l.scheme, l.addr)
	return l.Listener.Close()
}

// Unwrap is here just in case the user needs it. They'll need to assert it to
// their own `interface { Unwrap() net.Listener }`, but it's there.
func (l listener) Unwrap() net.Listener {
	return l.Listener
}

// cleanup detects the scheme to do appropriate clean up operations.
func cleanup(scheme, addr string) error {
	switch scheme {
	case "unix", "unixpacket":
		// Try and Lstat the socket file. If it doesn't exist, then don't
		// bother.
		s, err := os.Lstat(addr)
		if err != nil {
			return nil
		}

		// Check if the file is a socket file. Only remove it if it is.
		if s.Mode()&os.ModeSocket != 0 {
			return os.Remove(addr)
		}

		// Error out if it's not.
		return fmt.Errorf("file %q is not a Unix socket", addr)
	}

	return nil
}
