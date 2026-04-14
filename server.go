package main

//===================================================================================
//testing this proxy:
//1. create a backend server in one terminal: python3 -m http.server 8081
//2. run the proxy in a different terminal: go run server.go
//3. create an HTTP request from the client side to test it all out in a different terminal: curl http://localhost:8080\
//4. to test the concurrency, you can send a burst of HTTP request at once: for ($i = 0; $i -lt 50; $i++) { curl.exe -s http://localhost:8080 | Out-Null }
//5. to test the load balancing, create 3 backend servers on three different ports and then run the burst of HTTP requests
//6 Now with docker, the above step is not needed. Run docker-compose up --build and then send the burst of HTTP requests to the proxy
//to test the health checks, run docker compose stop backend2 and u will see the load balancer and health checks at work
//7. To test everything with the rate limiting: $urls = @("/testFiles/", "/testFiles/about.html", "/testFiles/contact.html", "/testFiles/api.html", "/testFiles/home.html")
// >> 1..50 | ForEach-Object {
// >>     $url = $urls[$_ % $urls.Length]
// >>     curl.exe -s "http://localhost:8080$url" | Out-Null
//8. To test HTTPS and redirection with HTTP, you can send a request to the HTTP port and see that it redirects to HTTPS: curl -i http://localhost:8080/testFiles/about.html
// Then do curl.exe -k https://localhost:8443
//===================================================================================

//List of things that have been finished:
//1. Created a Reverse Proxy in Go that forads requests from clients to backend sevrer
//2. Editted it up so its now  a concurrent reverse proxy that can take in multiple requests at the same time, did that by imementing multi-threading with a worker pool (find right phasing for that)
//3. implemented a Worker/Thread pool pattern, buffered channel queue, TCP netwokring and HTTP parsing in Go,
//4. Implmented a Round-Roboin algerithom for load balancing
//5. Implemented containerization for the proxy and backend servers with Docker
//6. Created the LRU cache and Token Bucket rate limiter
//7. Added TLS support for secure HTTPS connections, and also added a HTTP to HTTPS redirection
import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync/atomic"
	"time"
	"bytes"
)

// BackendServer represents a server we're proxying traffic to
type BackendServer struct {
	Host              string
	Port              string
	Healthy           bool
	ActiveConnections int32
	FailureCount      int32
}

// this is a struct, which is a custom data type that groups related fields like a class,
// but without methods
type Request struct {
	Method  string
	URL     string
	Host    string
	Headers map[string]string //hashMap
	Body    []byte            //a byte slice, which is a dynamic array of bytes
}

// var backends = []BackendServer{
// 	{Host: "localhost", Port: "8080", Healthy: true},
// 	{Host: "localhost", Port: "8080", Healthy: true},
// 	{Host: "localhost", Port: "8080", Healthy: true},
// }

var backends = []BackendServer{
	{Host: "backend1", Port: "8080", Healthy: true},
	{Host: "backend2", Port: "8080", Healthy: true},
	{Host: "backend3", Port: "8080", Healthy: true},
}

var counter uint64
var cache *Cache
var rateLimiter *RateLimiter

func main() {

	cache = NewCache(100)
	cache.StartCleanup(60 * time.Second)
	rateLimiter = createRateLimiter()

	//load tls certificate and key from files, this is used for HTTPS connections, the cert.pem file contains the public key and some other information about the certificate, while the key.pem file contains the private key used to prove identity and encrypt/decrypt data
	cert, error := tls.LoadX509KeyPair("cert.pem", "key.pem")
	if error != nil {
		log.Fatalf("Failed to load TLS certificate: %v", error)
	}
	//says create a TLS configuration using this certification and only allow secure TLS versions
	tlsConfig := &tls.Config{
		//dynamic array of certificates
		Certificates: []tls.Certificate{cert},
		//this is the specific version of TLS to use
		MinVersion: tls.VersionTLS12,
	}

	//this is the same as the net.Listen but for TLS connections, it creates a secure listener on port 8443 using the TLS configuration we just set up, this allows the proxy to accept HTTPS connections from clients
	tlsListener, error := tls.Listen("tcp", ":8443", tlsConfig)
	if error != nil {
		log.Fatal("Failed to start TLS listener:", error)
	}
	defer tlsListener.Close()

	//opening a TCP socket on port 8080 for this specific proxy server to listen for incoming connections
	// from clients, and it returns a listener object that we can use to accept connections
	//redirects only
	httpListener, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Fatal("Failed to start HTTP listener:", err)
	}
	defer httpListener.Close()

	fmt.Println("Proxy listening on :8080 (HTTP → redirects to HTTPS)")
	fmt.Println("Proxy listening on :8443 (HTTPS)")

	//net.Conn is Go's interface for representing a network connection, it provides methods for reading and writing data over the connection
	//this ius a channel in GO, which is a buffered (limited/offiucial size) deque in python that can do synchronization
	// (make sure multiple goroutines can coodinate execution and not run ahead of each other) for goroutines automatically,
	//which means when the queue is empty and it needs a connection, it will wait and not error out
	//creates a channel to hold incoming connections, with a buffer size of 100
	//how buffering works here: if the channel is unbuffered, it has a capacity of zero, which means it cant hold any connection
	//that means when we try to insert into the channel, it blocks it until a different goroutine is ready to recieve from that channel
	//when its buffered, that means it has capacity to hold a specific number of connection, but once its full it has to block like its
	//unbuffered until a goroutine recieves from the channel to maske space
	queue := make(chan net.Conn, 100)

	for i := 0; i < 10; i++ {
		//keyword 'go' makes this function concurrent, allkowing for multi-threading
		//the 'go' keyword spins up a goroutine, which is goLang's version for a lightweight thread
		//basically says run this function concurrently and dont wait for it to finish, allows for thousands of threads to be ran with
		//with very little memopry overhead. a traditional thread used 1 MB while a goroutine starts at 2 KB and can grow or shrink
		//based on the needs of the thread
		go func() {
			//how to iterate over a queue/channel in Go
			//it takes connections and if there are none then it waits until there is one, and then it handles the connection and moves on to the next one
			for connection := range queue { //this is just the queue of requests beiong shared by the workers/threads
				handleConnection(connection)
				//the thread will run this function and then come back to the for loop for ther next request
			}
		}() //this is used to call the function right away
	}

	startHealthChecks()

	//spins up a background process to redirect HTTP requests to the HTTPS port
	go func() {
		for {
			connection, error := httpListener.Accept()
			if error != nil {
				log.Printf("Error accepting connection: %v", error)
				continue
			}

			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1024)
				c.Read(buf)
				c.Write([]byte("HTTP/1.1 301 Moved Permanently\r\nLocation: https://localhost:8443\r\n\r\n"))
			}(connection)
		}

	}()

	for {
		//.Accept() waits for a client to connect and once they do, it returns a net.Conn object representing the connection
		connection, error := tlsListener.Accept()
		if error != nil {
			log.Printf("Error accepting connection: %v", error)
			continue
		}
		//select default is Go's non-blocking send pattern
		select {
		//tries to send connection to channel, but if full default branch runs instead, called backpressure
		//go is designed so that workers/threads don;t need to be manually synchronized.
		//we just give it the queue of connection requests and it haddles which works tke it over
		case queue <- connection:

		default:
			connection.Write([]byte("HTTP/1.1 503 Service Unavailable\r\n\r\nServer is busy, try again later"))
			connection.Close()
			log.Println("Connection refused: server is busy")
		}
	}
}

// net.Conn is GO's interface for a network connection
func handleConnection(clientConn net.Conn) {

	defer clientConn.Close()

	//this returnns the client's IP address, port, and any error that occurs while parsing the address
	clientIP, _, error := net.SplitHostPort(clientConn.RemoteAddr().String())
	if error != nil {
		clientIP = clientConn.RemoteAddr().String() //if there is an error, just use the whole address as the IP
	}

	if !rateLimiter.isAllowed(clientIP) {
		log.Printf("Rate limited: %s", clientIP)
		clientConn.Write([]byte("HTTP/1.1 429 Too Many Requests\r\n\r\nRate limit exceeded, try again later"))
		return
	}

	reader := bufio.NewReader(clientConn)
	data, _ := reader.Peek(1024)
	//checking if the request requires gRPC
	if isgRPC(data){
		log.Printf("Detected gRPC request, forwarding to gRPC backend")
		forwardTogRPCBackend(clientConn, reader)
		return
	}

	//parse the incoming HTTP Request
	request, rawRequest, error := parseHTTPRequest(reader)
	if error != nil {
		log.Println("Failed to parse request:", error)
		return
	}

	fmt.Printf("Recieved  %s %s (Host: %s)\n", request.Method, request.URL, request.Host)

	

	//checks the cache
	if request.Method == "GET" {
		if cached, ok := cache.Get(request.URL); ok {
			log.Printf("Cache hit for %s", request.URL)
			clientConn.Write(cached)
			cache.Stats()
			return
		}

	}

	response, error := forwardRequestToBackend(rawRequest)
	if error != nil {
		log.Println("Failed to forward to backend:", error)
		clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\nBackend unavailable"))
		return
	}

	if request.Method == "GET" {
		cache.Put(request.URL, response, 30*time.Second)
		log.Printf("Cache MISS, stored: %s", request.URL)
	}

	//sends the response back to the client
	clientConn.Write(response)
	fmt.Println("Response forwarded back to client")


}

// the purpose of this function is raw HTTP request a strcutured representation of the request and the original raw bytes
// literally just a function to reformate the request
func parseHTTPRequest(reader *bufio.Reader) (*Request, []byte, error) {

	//a reader that reads the network connection line by line
	var rawLines []string
	//basically a hashmap called Headersto store all the infomration about the request
	//this makes a request object and request is pointing to the address of that object
	request := &Request{
		Headers: make(map[string]string),
	}

	//read bytes until a newline character is found
	//this will contain the Method Header, path, and version of the HTTP request
	firstLine, error := reader.ReadString('\n')
	if error != nil {
		return nil, nil, error
	}
	//removes whitespace
	firstLine = strings.TrimSpace(firstLine)
	rawLines = append(rawLines, firstLine)

	//splits the first line/header into its groups
	//parts = ["GET", "/index.html", "HTTP/1.1"]
	parts := strings.Split(firstLine, " ")
	if len(parts) >= 2 {
		request.Method = parts[0]
		request.URL = parts[1]
	}

	for {
		//read characters from network connection until you see a newline (\n)
		line, error := reader.ReadString('\n')
		if error != nil {
			break
		}
		//save the new line
		rawLines = append(rawLines, line)
		//remove whitespaces
		line = strings.TrimSpace(line)

		//detects the end of bthe headers
		if line == "" {
			break
		}

		colonIndex := strings.Index(line, ":")
		if colonIndex > 0 {

			key := strings.TrimSpace(line[:colonIndex])
			value := strings.TrimSpace(line[colonIndex+1:])
			request.Headers[key] = value

			if strings.ToLower(key) == "host" {
				request.Host = value
			}
		}
	}

	rawRequest := []byte(strings.Join(rawLines, "\r\n"))
	return request, rawRequest, nil
}

func forwardRequestToBackend(rawRequest []byte) ([]byte, error) {

	backend := getNextBackend()
	if backend == nil {
		return nil, fmt.Errorf("No healthy backends available")
	}
	log.Printf("Forwarding to backend %s:%s", backend.Host, backend.Port)

	//creates a TCP connection to the backend server
	//uses ther Host name (IP address or domain name that DNS translates to IP) and Port (process or service on that machine to talk to)
	// to make the connectionm
	backendConnection, error := net.Dial("tcp", backend.Host+":"+backend.Port)
	if error != nil {
		return nil, fmt.Errorf("Failed to connect to backend: %w", error)
	}

	defer backendConnection.Close()

	//sends the request toi the backend
	_, error = backendConnection.Write(rawRequest)
	if error != nil {
		return nil, fmt.Errorf("could not write to backend: %w", error)
	}

	//reads everytyhing the backend sends
	response, error := io.ReadAll(backendConnection)
	if error != nil {
		return nil, fmt.Errorf("could not read from backend: %w", error)
	}
	return response, nil

}

// implements the Round Robin ALgo to select the next backend server to forward the request to (Load Balancer)
func getNextBackend() *BackendServer {

	for i := 0; i < len(backends); i++ {

		index := atomic.AddUint64(&counter, 1) % uint64(len(backends))

		if backends[index].Healthy {
			return &backends[index]
		}

	}
	return nil
}

//if the bytes contain application/grpc, then its gRPC
func isgRPC(data []byte) bool{
	
	return bytes.Contains(data, []byte("application/grpc"))
}

func forwardTogRPCBackend(clientConn net.Conn, reader *bufio.Reader){

	backendConn, error := net.Dial("tcp", "grpc_backend:9091")
	if error != nil {
		log.Printf("Failed to connect to gRPC backend: %v", error)
		return
	}
	defer backendConn.Close()

	go func(){
		io.Copy(backendConn, clientConn)
	}()

	//reads chuckns from the right one to the left one, and then sends it back to the client, this is how we can forward gRPC requests without needing to parse them
	//we have the connection made both ways to the client and the backend, so we can just copy data between them without needing to understand the content of the data, which is important for gRPC because its a binary protocol and not human readable like HTTP
	io.Copy(clientConn, backendConn)
	log.Println("gRPC request forwarded and response sent back to client")
}

func startHealthChecks() {
	//starts a goroutine
	go func() {
		time.Sleep(2 * time.Second)
		for {
			for index := range backends {
				//this is the health check, it opens a TCP connection to the backend
				//it takjes the protocall, address, and a timeout duration fo rit to wait for a connection
				connection, error := net.DialTimeout("tcp", backends[index].Host+":"+backends[index].Port, 2*time.Second)
				if error != nil {
					backends[index].Healthy = false
					log.Printf("Backend %s:%s is down", backends[index].Host, backends[index].Port)
				} else {
					backends[index].Healthy = true
					connection.Close()
				}
			}
			time.Sleep(10 * time.Second)
		}
	}()
}
