package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

const errorResponse = "HTTP/1.1 501 Internal Server Error\r\n" +
	"Content-Type: text/plain\r\n" +
	"Content-Length: 14\r\n" +
	"\r\n" +
	"Internal Error"

var (
	tunnelSemaphore = make(chan struct{}, 100) // Limit to 10 concurrent tunnels
)

var strAddr, username, password, serverHost string

func main() {
	if len(os.Args) < 4 {
		log.Fatal("not enough arguments given")
	}

	strAddr, username, password = os.Args[1], os.Args[2], os.Args[3]
	serverHost = strings.Split(strAddr, ":")[0]

	conn, err := net.Dial("tcp", strAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	fmt.Fprintf(conn, fmt.Sprintf("%s:%s\n", username, password))

	ok, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		log.Fatalf("Unable to connect: %s", err)
	}
	if strings.TrimSpace(ok) != "ok" {
		log.Fatalf("Unable to connect: no 'ok' response")
	}

	fmt.Println("Connected to the server")

	for {
		msg, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			log.Printf("Error receiving message from server: %s", err)
			break
		}

		data := strings.Fields(strings.TrimSpace(msg))

		if len(data) != 4 || data[0] != "port" || data[2] != "connect" {
			log.Println("Unsupported or malformed request from server")
			continue
		}

		log.Printf("Connecting to %s from server port %s", data[3], data[1])
		go handleTunnel(data[1], data[3])
	}
}

func handleTunnel(port string, host string) {
	tunnelSemaphore <- struct{}{}        // Acquire a slot
	defer func() { <-tunnelSemaphore }() // Release the slot

	time.Sleep(100 * time.Millisecond)
	srcConn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%s", serverHost, port), 10*time.Second)
	if err != nil {
		log.Printf("Unable to connect to server tunnel: %s", err)
		return
	}
	defer srcConn.Close()

	destConn, err := net.DialTimeout("tcp", host, 10*time.Second)
	if err != nil {
		log.Printf("Unable to connect to destination: %s", err)
		fmt.Fprintf(srcConn, errorResponse)
		return
	}
	defer destConn.Close()

	srcConnStr := fmt.Sprintf("%s->%s", srcConn.LocalAddr().String(), srcConn.RemoteAddr().String())
	dstConnStr := fmt.Sprintf("%s->%s", destConn.LocalAddr().String(), destConn.RemoteAddr().String())

	log.Printf("src_conn: %s - dst_conn: %s\n", srcConnStr, dstConnStr)

	var wg sync.WaitGroup

	wg.Add(2)
	go transfer(&wg, destConn, srcConn, dstConnStr, srcConnStr)
	go transfer(&wg, srcConn, destConn, srcConnStr, dstConnStr)
	wg.Wait()
	log.Println("Tunnel closed")
}

func transfer(wg *sync.WaitGroup, destination io.Writer, source io.Reader, destName, srcName string) {
	defer wg.Done()
	written, err := io.Copy(destination, source)
	if err != nil {
		fmt.Printf("Error during copy from %s to %s: %v\n", srcName, destName, err)
	}
	log.Printf("copied %d bytes from %s to %s\n", written, srcName, destName)
}
