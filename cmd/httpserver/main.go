// Package httpserver
package main

import (
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"httpfromtcp/internal/diagnostic"
	"httpfromtcp/internal/headers"
	"httpfromtcp/internal/request"
	"httpfromtcp/internal/response"
	"httpfromtcp/internal/router"
	"httpfromtcp/internal/server"
)

const port = 42069

func toStr(bytes []byte) string {
	var out strings.Builder
	for _, b := range bytes {
		fmt.Fprintf(&out, "%02x", b)
	}

	return out.String()
}

// TODO: not sure if we should put this chunked stuff in the chunked.go
var chunkedRequest router.Handler = func(w *response.Writer, req *request.Request) {
	// Every canned body in this handler is HTML; /video overrides.
	h := w.Header()

	h.Replace("content-type", "text/html")

	val, ok := req.PathValue("path")
	if !ok || val == "" {
		w.Error("no path", response.StatusNotFound, "text/plain")
		return
	}
	resp, err := http.Get("https://httpbin.org/" + val)
	defer resp.Body.Close()

	if err != nil {
		w.Error("", response.StatusBadRequest, "")
		return
	}

	if resp.Body == nil {
		w.Error("", response.StatusInternalServerError, "")
	}

	_ = w.WriteStatusLine(response.StatusOK)

	h.Set("transfer-encoding", "chunked")
	h.Delete("Content-length")
	h.Replace("content-type", "text/html")
	h.Set("Trailer", "X-Content-SHA256")
	h.Set("Trailer", "X-Content-Length")
	_ = w.WriteHeaders()

	fullBody := []byte{}

	for {
		data := make([]byte, 32)
		n, err := resp.Body.Read(data)
		if err != nil {
			break
		}
		fullBody = append(fullBody, data[:n]...)

		_, _ = w.WriteChunkedBody(data[:n])
	}

	tailers := headers.NewHeaders()
	shaSig := sha256.Sum256(fullBody)

	tailers.Set("X-Content-SHA256", toStr(shaSig[:]))
	tailers.Set("X-Content-Length", fmt.Sprintf("%x", len(fullBody)))
	// WriteTrailers terminates the chunked stream itself
	// ("0\r\n" + trailers + "\r\n") — no Done call before it.
	_ = w.WriteTrailers(tailers)
}

var myProblem router.Handler = func(w *response.Writer, req *request.Request) {
	body := `<html>
  <head>
    <title>500 Internal Server Error</title>
  </head>
  <body>
    <h1>Internal Server Error</h1>
    <p>Okay, you know what? This one is on me.</p>
  </body>
</html>`

	w.Error(body, response.StatusInternalServerError, "text/html")
}

var yourProblem router.Handler = func(w *response.Writer, req *request.Request) {
	body := `<html>
  <head>
    <title>400 Bad Request</title>
  </head>
  <body>
    <h1>Bad Request</h1>
    <p>Your request honestly kinda sucked.</p>
  </body>
</html>`

	w.Error(body, response.StatusBadRequest, "text/html")
}

var rootEndpoint router.Handler = func(w *response.Writer, req *request.Request) {
	body := []byte(`<html>
  <head>
    <title>200 OK</title>
  </head>
  <body>
    <h1>Success!</h1>
    <p>Your request was an absolute banger.</p>
  </body>
</html>`)

	w.Header().Set("Content-type", "text/html")
	_, err := w.WriteBody(body)
	if err != nil {
		fmt.Println(err)
	}
}

var uploadFile router.Handler = func(w *response.Writer, req *request.Request) {
	req.Trailers.ForEach(func(n string, v string) {
		fmt.Printf("%s: %s\n", n, v)
	})
}

func MiddlewareTest(next router.Handler) router.Handler {
	return router.Handler(func(w *response.Writer, req *request.Request) {
		w.Header().Set("hehe", "haha")

		next(w, req)
	})
}

func PanicHandlerMiddleware(next router.Handler) router.Handler {
	return router.Handler(func(w *response.Writer, req *request.Request) {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("Recovered in Request ID: %v\n%+v", req.ID, r)
				w.Error(r.(string), response.StatusInternalServerError, "text/plain")
			}
		}()

		next(w, req)
	})
}

func main() {
	r := router.New()
	r.Use(PanicHandlerMiddleware).
		Use(MiddlewareTest).
		Get("/", rootEndpoint).
		Get("/myproblem", myProblem).
		Get("/yourproblem", yourProblem).
		Get("/httpbin/*path", chunkedRequest).
		Post("/upload-file", uploadFile).
		Get("/panic", func(w *response.Writer, req *request.Request) {
			panic("hehe")
		}).
		Get("/json/*name", router.Handler(func(w *response.Writer, req *request.Request) {
			val, ok := req.PathValue("name")
			if !ok {
				w.Error("no name", response.StatusBadRequest, "text/plain")
				return
			}
			w.WriteJSON(map[string]any{
				"name":   val,
				"peepee": 2,
			}, response.StatusOK)
		}))

	r.NotFound(func(w *response.Writer, req *request.Request) {
		errJSON := map[string]any{
			"error": "not found",
		}
		w.WriteJSON(errJSON, response.StatusNotFound)
	})

	r.MethodNotAllowed(func(w *response.Writer, req *request.Request) {
		_ = w.WriteStatusLine(response.StatusMethodNotAllowed)
	})

	r.Build()

	if err := r.Err(); err != nil {
		log.Fatalf("Error starting server: %v", err)
	}

	server, err := server.Serve(port, r)
	if err != nil {
		log.Fatalf("Error starting server: %v", err)
	}

	defer func() {
		_ = server.Close()
	}()
	fmt.Println(diagnostic.Underline("/files/*path/edit", 99, 120))
	log.Println("Server started on port", port)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	log.Println("Server gracefully stopped")
}
