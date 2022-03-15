package listener

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

	"github.com/pkg/errors"
)

// DefaultListenConfig is the default ListenConfig used for Listen().
var DefaultListenConfig = &net.ListenConfig{}

// HTTPShutdownTimeout is the timeout to wait when shutting down HTTP.
var HTTPShutdownTimeout = 5 * time.Second

// MustHTTPListenAndServe is HTTPListenAndServe that log.Fatals on an error.
func MustHTTPListenAndServe(address string, handler http.Handler) {
	if err := HTTPListenAndServe(address, handler); err != nil {
		log.Fatalln("cannot listen and serve HTTP:", err)
	}
}

// HTTPListenAndServe listens and serves HTTP until a SIGINT is received.
func HTTPListenAndServe(address string, handler http.Handler) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)
		<-sigint

		cancel()
	}()

	return HTTPListenAndServeCtx(ctx, &http.Server{Addr: address, Handler: handler})
}

// MustHTTPListenAndServeCtx is HTTPListenAndServeCtx that log.Fatals on an error.
func MustHTTPListenAndServeCtx(ctx context.Context, server *http.Server) {
	if err := HTTPListenAndServeCtx(ctx, server); err != nil {
		log.Fatalln("cannot listen and serve HTTP:", err)
	}
}

// HTTPListenAndServeCtx listens to the address set in http.Server and serves
// HTTP. It gracefully exits if the given ctx expires.
func HTTPListenAndServeCtx(ctx context.Context, server *http.Server) error {
	l, err := Listen(server.Addr)
	if err != nil {
		return err
	}
	defer l.Close()

	var serveErr = make(chan error, 1)
	go func() {
		serveErr <- server.Serve(l)
	}()

	select {
	case <-ctx.Done():
		// Context finish is not a fatal error.
	case serveErr := <-serveErr:
		if serveErr != http.ErrServerClosed {
			err = serveErr
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), HTTPShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return errors.Wrap(err, "failed to shutdown gracefully")
	}

	return err
}

// Listen calls ListenWithConfig with the DefaultListenConfig.
func Listen(address string) (net.Listener, error) {
	return ListenWithConfig(DefaultListenConfig, address)
}

// ListenWithConfig listens for incoming connections using the given address
// string. The address can be formatted as such:
//
//    127.0.0.1:29485 (implicitly tcp)
//    tcp://127.0.0.1:29485
//    tcp4://127.0.0.1:29485
//    unix:///tmp/path/to/socket.sock (will be automatically cleaned up)
//    unixpacket:///tmp/path/to/socket.sock (same as unix)
func ListenWithConfig(config *net.ListenConfig, address string) (net.Listener, error) {
	if !strings.Contains(address, "://") {
		address = "tcp://" + address
	}

	listenURL, err := url.Parse(address)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse address")
	}

	var (
		scheme = listenURL.Scheme
		addr   = listenURL.Host + listenURL.Path
	)

	// Do cleanup just in case.
	if err := cleanup(scheme, addr); err != nil {
		return nil, errors.Wrap(err, "failed to run cleanup prior to listening")
	}

	l, err := config.Listen(context.Background(), scheme, addr)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to listen at %v", listenURL)
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
