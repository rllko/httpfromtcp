package main

import (
	"fmt"
	"log"
	"net"

	"httpfromtcp/internal/request"
)

func main() {
	f, err := net.Listen("tcp", ":42069")

	defer func() {
		err := f.Close()
		if err != nil {
			log.Printf("Error closing: %s", err)
		}
	}()

	if err != nil {
		log.Fatalf("Error opening the file: %s", err.Error())
	}

	for {
		conn, err := f.Accept()
		if err != nil {
			log.Fatalf("Error accepting the connection: %s", err.Error())
		}

		r, err := request.RequestFromReader(conn)
		if err != nil {
			log.Fatal("error", "error", err)
		}

		fmt.Printf("Request line:\n")
		fmt.Printf("- Method: %s\n", r.RequestLine.Method)
		fmt.Printf("- Target: %s\n", r.RequestLine.RequestTarget)
		fmt.Printf("- Version: %s\n", r.RequestLine.HTTPVersion)

		fmt.Printf("Headers:\n")

		r.Headers.ForEach(func(n, v string) {
			fmt.Printf("- %s: %s\n", n, v)
		})

		fmt.Printf("%v", r.Body)
	}
}
