# hserve

A small library to provide TCP+Unix listener and HTTP server functions with
graceful shutdowns and context cancellation support.

## Usage

```go
// This function will block until SIGINT is received or HTTP servers error out.
hserve.MustListenAndServe(ctx, "unix:///tmp/server.sock", handler)
```
