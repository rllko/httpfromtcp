# httpfromtcp

[![Go](https://github.com/rllko/httpfromtcp/actions/workflows/go.yml/badge.svg)](https://github.com/rllko/httpfromtcp/actions/workflows/go.yml)

A zero-dependency HTTP/1.1 server built on raw TCP sockets using only the Go standard library. 

This project was built from scratch as a deep dive into network protocol implementations, state-machine parsers, and zero-allocation routing.

> ⚠️ **Educational Project:** Designed for learning and protocol study. Do not use in production.

---

## Key Technical Highlights

* **Resumable State-Machine Parser:** Non-blocking HTTP/1.1 parser conforming to RFC 9110/9112 that handles partial reads and chunked transfer encodings cleanly across split TCP packets.
* **Trie-Based Router:** Supports static paths, path parameters (`{id}`), and trailing wildcards (`*path`). 
* **RFC-Compliant Framing:** Enforces strict HTTP request-smuggling mitigations, dynamic memory limits, and socket deadlines.
* **Rust-Style Diagnostics:** Custom compile-time-style route error formatting pointing to invalid path syntax directly in your terminal.

> 📖 **Curious how it works under the hood?** 
> Read [ARCHITECTURE.md](./ARCHITECTURE.md) for a deep dive into the custom parser state machine, trie router mechanics, frame validation, and buffer management.

---

## Quickstart

### Prerequisites

* Go 1.26 or later

### Running the Example Server

```bash
# Clone and build
git clone [https://github.com/rllko/httpfromtcp.git](https://github.com/rllko/httpfromtcp.git)
cd httpfromtcp
go build ./...

# Run the demo server (listens on :42069)
go run ./cmd/httpserver
```

Test it in a separate terminal:
```bash
curl http://localhost:42069/
```

---

## Usage

```go
package main

import (
	"log"

	"httpfromtcp/internal/request"
	"httpfromtcp/internal/response"
	"httpfromtcp/internal/router"
	"httpfromtcp/internal/server"
)

func main() {
	r := router.New()

	r.Get("/", func(w response.Writer, req *request.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteBody([]byte("Hello from raw TCP!"))
	})

	r.Get("/users/{id}", func(w response.Writer, req *request.Request) {
		id, _ := req.PathValue("id")
		w.WriteJSON(map[string]string{"user_id": id}, response.StatusOK)
	})

	// Fail fast on invalid route syntax at startup
	if errs := r.Err(); len(errs) > 0 {
		for _, err := range errs {
			log.Println(err)
		}
		log.Fatal("Invalid route configuration")
	}

	srv, err := server.Serve(8080, r)
	if err != nil {
		log.Fatal(err)
	}
	defer srv.Close()

	select {}
}
```

---

## Testing & Benchmarks

The test suite includes split-read tests, malformed-input tests, and differential testing between router indexes.

```bash
# Run all unit and integration tests
go test ./...

# Run tests with race detection
go test -race ./...

# Run router and parser benchmarks
go test -bench . ./...
```

---

## Author's Note on AI Usage

This is a hand-written study project. I used AI as a tutor (explaining push vs. pull parsing, state machines, and RFC specifics) rather than a code generator. Each bug in this repository is a bug that I found and corrected myself to learn the HTTP/1.1 protocol at the byte level.

## Standards Compliance

* [RFC 9110 — HTTP Semantics](https://www.rfc-editor.org/rfc/rfc9110)
* [RFC 9112 — HTTP/1.1 Specification](https://www.rfc-editor.org/rfc/rfc9112)
* [RFC 3986 — URI Generic Syntax](https://www.rfc-editor.org/rfc/rfc3986)
