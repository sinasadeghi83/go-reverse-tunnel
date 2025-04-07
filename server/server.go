package server

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
)

var (
	conns           = make(map[string]net.Conn)
	mu              sync.Mutex
	tunnelSemaphore = make(chan struct{}, 100)
	accounts        = make(map[string]string)
)

func SetupAndListen(clientPort, proxyPort string) error {
	go listenForClient(clientPort)

	return http.ListenAndServe(
		":"+proxyPort,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodConnect {
				notifyClient(w, r)
			} else {
				http.Error(w, "Unsupported method", http.StatusMethodNotAllowed)
			}
		}),
	)
}

func AddAccounts(username, password string) {
	accounts[username] = password
}

func listenForClient(clientPort string) {
	ln, err := net.Listen("tcp", ":"+clientPort)
	if err != nil {
		log.Fatalf("Error starting client listener: %s", err)
	}
	defer closeAllConns()

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Error accepting client connection: %s", err)
			continue
		}
		go handleClientConnection(conn)
	}
}

func handleClientConnection(conn net.Conn) {
	credentials, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		log.Printf("Error reading credentials: %s", err)
		return
	}
	credParams := strings.Split(strings.TrimSpace(credentials), ":")
	if len(credParams) < 2 {
		log.Printf("Error decoding credentials: %s", err)
	}

	username, _ := credParams[0], credParams[1]
	mu.Lock()
	conns[username] = conn
	mu.Unlock()

	log.Printf("Client connected: %s", username)
	conn.Write([]byte("ok\n"))
}

func validateProxyAuth(authHeader string) (username string, password string, authenticated bool) {
	authenticated = false
	username, password = "", ""
	if authHeader == "" || !strings.HasPrefix(authHeader, "Basic ") {
		return
	}

	// Decode Base64 credentials
	encodedCredentials := strings.TrimPrefix(authHeader, "Basic ")
	decodedBytes, err := base64.StdEncoding.DecodeString(encodedCredentials)
	if err != nil {
		return
	}

	// Split the decoded string into username and password
	credentials := strings.SplitN(string(decodedBytes), ":", 2)
	if len(credentials) != 2 {
		return
	}
	username = credentials[0]
	password = credentials[1]

	mu.Lock()
	if _, exists := conns[username]; exists {
		authenticated = true
	}
	mu.Unlock()

	return
}

func notifyClient(w http.ResponseWriter, r *http.Request) {
	proxAuth := r.Header.Get("Proxy-Authorization")
	username, _, authenticated := validateProxyAuth(proxAuth)
	if !authenticated {
		w.Header().Set("Proxy-Authenticate", `Basic realm="Restricted"`)
		http.Error(w, "407 Proxy Authentication Required", http.StatusProxyAuthRequired)
		return
	}

	mu.Lock()
	conn := conns[username]
	mu.Unlock()

	var ln net.Listener
	for port := 30000; port < 40000; port++ {
		var err error
		ln, err = net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			// Successfully bound to port
			break
		}
	}
	defer ln.Close()
	lnAddr := ln.Addr().(*net.TCPAddr)

	log.Printf("Establishing tunnel for username '%s' to host '%s' through port '%d'", username, r.Host, lnAddr.Port)

	_, err := conn.Write([]byte(fmt.Sprintf("port %d connect %s\n", lnAddr.Port, r.Host)))
	if err != nil {
		http.Error(w, "could not transfer data with client", http.StatusNotFound)
		log.Println("could not transfer data with client", err)
	}

	destConn, err := ln.Accept()
	if err != nil {
		http.Error(w, "client could not connect", http.StatusNotFound)
		log.Println("client could not connect error:", err)
	}
	defer destConn.Close()

	handleTunnel(w, r, destConn)
}

func handleTunnel(w http.ResponseWriter, r *http.Request, destConn net.Conn) {
	tunnelSemaphore <- struct{}{}        // Acquire a slot
	defer func() { <-tunnelSemaphore }() // Release the slot
	w.WriteHeader(http.StatusOK)

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "webserver doesn't support hijacking", http.StatusInternalServerError)
		return
	}

	srcConn, _, err := hj.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer srcConn.Close()

	srcConnStr := fmt.Sprintf("%s->%s", srcConn.LocalAddr().String(), srcConn.RemoteAddr().String())
	dstConnStr := fmt.Sprintf("%s->%s", destConn.LocalAddr().String(), destConn.RemoteAddr().String())

	log.Printf("%s - %s - %s\n", r.Proto, r.Method, r.Host)
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

func closeAllConns() {
	mu.Lock()
	defer mu.Unlock()
	for _, conn := range conns {
		conn.Close()
	}
}
