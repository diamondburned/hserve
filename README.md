# listener

A small library to provide TCP+Unix listener functions as well as graceful
shutdowns.

## Usage

```go
server := http.Server{
	Addr:    "unix:///tmp/server.sock",
	Handler: handler,
}

// This function will block until SIGINT is received or HTTP servers error out.
if err := listener.HTTPListenAndServe(&server); err != nil {
	log.Fatalln("Failed to serve HTTP:", err)
}
```
