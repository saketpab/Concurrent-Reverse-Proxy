package main
//===================================================================================
//testing this proxy:
//1. create a backend server in one terminal: python3 -m http.server 8081
//2. run the proxy in a different terminal: go run server.go
//3. create an HTTP request from the client side to test it all out in a different terminal: curl http://localhost:8080\
//4. to test the concurrency, you can send a burst of HTTP request at once: seq 1 50 | xargs -n1 -P50 curl -s http://localhost:8080 > /dev/null
//===================================================================================

//List of things that have been finished:
//1. Created a Reverse Proxy in Go that forads requests from clients to backend sevrer 
//2. Editted it up so its now  a concurrent reverse proxy that can take in multiple requests at the same time, did that by imementing multi-threading with a worker pool (find right phasing for that)
//3. implemented a Worker/Thread pool pattern, buffered channel queue, TCP netwokring and HTTP parsing in Go, 

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
)

// BackendServer represents a server we're proxying traffic to
type BackendServer struct{
	Host string
	Port string	
}

//this is a struct, which is a custom data type that groups related fields like a class, 
//but without methods 
type Request struct{
	Method string
	URL string
	Host string
	Headers map[string]string //hashMap
	Body []byte //a byte slice, which is a dynamic array of bytes 
}

var backend = BackendServer{
	Host: "localhost",
	Port: "8081",
}

func main() {

	//opening a TCP socket on port 8080
	listener, error := net.Listen("tcp", ":8080")

	if error != nil{
		log.Fatalf("Error starting server: %v", error)
	}

	//keyword defer schedules a function call when the surrounding function returns 
	//the .Close function will release the port that we opened
	defer listener.Close()

	fmt.Println("Proxy listening on :8080")
	fmt.Printf("Forwarding to backend at %s:%s\n", backend.Host, backend.Port)

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

	for i:=0; i<10; i++{
		//keyword 'go' makes this function concurrent, allkowing for multi-threading 
		//the 'go' keyword spins up a goroutine, which is goLang's version for a lightweight thread
		//basically says run this function concurrently and dont wait for it to finish, allows for thousands of threads to be ran with
		//with very little memopry overhead. a traditional thread used 1 MB while a goroutine starts at 2 KB and can grow or shrink
		//based on the needs of the thread  
		go func(){
			//how to iterate over a queue/channel in Go
			//it takes connections and if there are none then it waits until there is one, and then it handles the connection and moves on to the next one
			for connection := range queue{ //this is just the queue of requests beiong shared by the workers/threads 
				handleConnection(connection)
				//the thread will run this function and then come back to the for loop for ther next request
			}
		}()//this is used to call the function right away
	}

	for {
		//.Accept() waits for a client to connect and once they do, it returns a net.Conn object representing the connection
		connection, error := listener.Accept()
		if error != nil{
			log.Printf("Error accepting connection: %v", error)
			continue
		}
		//select default is Go's non-blocking send pattern
		select{
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

func handleConnection(clientConn net.Conn){

	defer clientConn.Close()

	//parse the incoming HTTP Request 
	request, rawRequest, error := parseHTTPRequest(clientConn)

	if error != nil{
		log.Println("Failed to parse request:", error)
		return
	}

	fmt.Printf("Recieved %s %s (Host: %s)\n", request.Method, request.URL, request.Host)

	response, error := forwardRequestToBackend(rawRequest)
	if error != nil{
		log.Println("Failed to forward to backend:", error)
		clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\nBackend unavailable"))
		return
	}
	//sends the response back to the client
	clientConn.Write(response)
	fmt.Println("Response forwarded back to client")

}
//the purpose of this function is raw HTTP request a strcutured representation of the request and the original raw bytes
//literally just a function to reformate the request
func parseHTTPRequest(conn net.Conn) (*Request, []byte, error){

	//a reader that reads the network connection line by line 
	reader := bufio.NewReader(conn)
	var rawLines []string 
	//basically a hashmap called Headersto store all the infomration about the request
	//this makes a request object and request is pointing to the address of that object 
	request := &Request{
		Headers: make(map[string]string),
	}

	//read bytes until a newline character is found
	//this will contain the Method Header, path, and version of the HTTP request
	firstLine, error := reader.ReadString('\n')
	if error != nil{
		return nil,nil,error
	}
	//removes whitespace
	firstLine = strings.TrimSpace(firstLine)
	rawLines = append(rawLines, firstLine)

	//splits the first line/header into its groups
	//parts = ["GET", "/index.html", "HTTP/1.1"]
	parts := strings.Split(firstLine, " ")
	if len(parts) >= 2{
		request.Method = parts[0]
		request.URL = parts[1]
	}

	for{
		//read characters from network connection until you see a newline (\n)
		line, error := reader.ReadString('\n')
		if error != nil{
			break
		}
		//save the new line
		rawLines = append(rawLines, line)
		//remove whitespaces
		line = strings.TrimSpace(line)


		//detects the end of bthe headers 
		if line == ""{
			break
		}

		colonIndex := strings.Index(line, ":")
		if colonIndex > 0{

			key := strings.TrimSpace(line[:colonIndex])
			value := strings.TrimSpace(line[colonIndex+1:])
			request.Headers[key] = value

			if strings.ToLower(key) == "host"{
				request.Host = value
			}
		}
	}

	rawRequest := []byte(strings.Join(rawLines, "\r\n"))
	return request, rawRequest, nil
}

func forwardRequestToBackend(rawRequest []byte) ([]byte, error){

	//creates a TCP connection to the backend server
	//uses ther Host name (IP address or domain name that DNS translates to IP) and Port (process or service on that machine to talk to)
	// to make the connectionm
	backendConnection, error := net.Dial("tcp", backend.Host + ":" + backend.Port)
	if error != nil{
		return nil, fmt.Errorf("Failed to connect to backend: %w", error)
	}

	defer backendConnection.Close()

	//sends the request toi the backend
	_, error = backendConnection.Write(rawRequest)
	if error != nil{
		return nil, fmt.Errorf("could not write to backend: %w", error)
	}

	//reads everytyhing the backend sends
	response, error := io.ReadAll(backendConnection)
	if error != nil{
		return nil, fmt.Errorf("could not read from backend: %w", error)
	}
	return response, nil 


}