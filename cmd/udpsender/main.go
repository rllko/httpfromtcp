package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
)

func main() {
	addr, err := net.ResolveUDPAddr("udp", ":42069")
	if err != nil {
		log.Fatalf("Error resolving address: %s", err.Error())
	}

	conn, _ := net.DialUDP("udp", nil, addr)

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print(">")
		input, _ := reader.ReadBytes('\n')

		conn.Write(input)

	}
}
