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

var chunkedRequest router.Handler = func(w response.Writer, req *request.Request) {
	h := response.GetDefaultHeaders(0)
	// Every canned body in this handler is HTML; /video overrides.
	h.Replace("content-type", "text/html")

	resp, err := http.Get("https://httpbin.org/" + req.URL.Path[len("/httpbin/"):])
	defer func() {
		err := resp.Body.Close()
		if err != nil {
			log.Fatalf("error closing the response body")
		}
	}()

	if err != nil {
		w.Error(string(""), response.StatusBadRequest, "text/plain")
		return
	}

	w.WriteStatusLine(response.StatusOK)

	h.Set("transfer-encoding", "chunked")
	h.Delete("Content-length")
	h.Replace("content-type", "text/html")
	h.Set("Trailer", "X-Content-SHA256")
	h.Set("Trailer", "X-Content-Length")
	w.WriteHeaders(h)

	fullBody := []byte{}

	for {
		data := make([]byte, 32)
		n, err := resp.Body.Read(data)
		if err != nil {
			break
		}
		fullBody = append(fullBody, data[:n]...)
		w.WriteChunkedBody(data[:n])
	}

	tailers := headers.NewHeaders()
	shaSig := sha256.Sum256(fullBody)

	tailers.Set("X-Content-SHA256", toStr(shaSig[:]))
	tailers.Set("X-Content-Length", fmt.Sprintf("%x", len(fullBody)))
	// WriteTrailers terminates the chunked stream itself
	// ("0\r\n" + trailers + "\r\n") — no Done call before it.
	w.WriteTrailers(tailers)
}

var videoEndpoint router.Handler = func(w response.Writer, req *request.Request) {
	video, _ := os.ReadFile("./assets/vim.mp4")
	w.RespondWithBody(
		response.StatusOK,
		"video/mp4",
		video,
	)
}

var myProblem router.Handler = func(w response.Writer, req *request.Request) {
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

var yourProblem router.Handler = func(w response.Writer, req *request.Request) {
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

var rootEndpoint router.Handler = func(w response.Writer, req *request.Request) {
	body := []byte(`<html>
  <head>
    <title>200 OK</title>
  </head>
  <body>
    <h1>Success!</h1>
    <p>Your request was an absolute banger.</p>
  </body>
</html>`)

	w.RespondWithBody(
		response.StatusOK,
		"text/html",
		body,
	)
}

var uploadFile router.Handler = func(w response.Writer, req *request.Request) {
	fmt.Printf("OUTPUT: %s\n", req.Body)
	req.Trailers.ForEach(func(n string, v string) {
		fmt.Printf("%s: %s\n", n, v)
	})
}

func main() {
	r := router.New()
	r.Get("/", rootEndpoint).
		Get("/myproblem", myProblem).
		Get("/yourproblem", yourProblem).
		Get("/httpbin/*path", chunkedRequest).
		Get("/video", videoEndpoint).
		Post("/upload-file", uploadFile)

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

	log.Println("Server started on port", port)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	log.Println("Server gracefully stopped")
}
