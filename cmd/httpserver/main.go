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
	"httpfromtcp/internal/server"
)

const port = 42069

func respond400() []byte {
	return []byte(`<html>
  <head>
    <title>400 Bad Request</title>
  </head>
  <body>
    <h1>Bad Request</h1>
    <p>Your request honestly kinda sucked.</p>
  </body>
</html>`)
}

func respond500() []byte {
	return []byte(`<html>
  <head>
    <title>500 Internal Server Error</title>
  </head>
  <body>
    <h1>Internal Server Error</h1>
    <p>Okay, you know what? This one is on me.</p>
  </body>
</html>`)
}

func respond200() []byte {
	return []byte(`<html>
  <head>
    <title>200 OK</title>
  </head>
  <body>
    <h1>Success!</h1>
    <p>Your request was an absolute banger.</p>
  </body>
</html>`)
}

func toStr(bytes []byte) string {
	var out strings.Builder
	for _, b := range bytes {
		fmt.Fprintf(&out, "%02x", b)
	}

	return out.String()
}

func main() {
	server, err := server.Serve(port, func(w response.Writer, req *request.Request) {
		body := respond200()
		h := response.GetDefaultHeaders(0)
		h.Replace("content-type", "text/html")
		status := response.StatusOK

		if strings.HasPrefix(req.RequestLine.RequestTarget, "/httpbin/") {
			target := req.RequestLine.RequestTarget

			resp, err := http.Get("https://httpbin.org/" + target[len("/httpbin/"):])
			defer func() {
				err := resp.Body.Close()
				if err != nil {
					log.Fatalf("error closing the response body")
				}
			}()

			if err != nil {
				body = respond500()
				status = response.StatusInternalServerError
			} else {
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
				w.WriteTrailers(tailers)
				return
			}
		} else {
			switch req.RequestLine.RequestTarget {
			case "/video":
				h.Replace("content-type", "video/mp4")
				video, _ := os.ReadFile("./assets/vim.mp4")
				body = video
			case "/yourproblem":
				h.Replace("content-type", "text/html")
				status = response.StatusBadRequest
				body = respond400()
			case "/myproblem":
				h.Replace("content-type", "text/html")
				status = response.StatusInternalServerError
				body = respond500()
			}

			h.Replace("Content-length", fmt.Sprintf("%d", len(body)))
			_ = w.WriteStatusLine(status)
			_ = w.WriteHeaders(h)
			_, _ = w.WriteBody(body)

		}
	})
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
