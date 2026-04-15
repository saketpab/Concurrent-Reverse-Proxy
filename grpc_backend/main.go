package main

import (
	"context"
	"fmt"
	"log"
	"net"

	pb "concurrent-reverse-proxy/gen/pb-go/proto"
	"google.golang.org/grpc"
)
//there is alreadya  Porcess method, but methods defined on the struct (like we have in Process) will always overwirte inherited methods
type server struct {
	//this is a struct that that gives use all the default imnplementation for gRPC server, but if we change something it wil take our changes 
	pb.UnimplementedBackendServiceServer
}

//for server objects, then it takes the inputs, and the output after that, and pb is the file (as an alias) that has these functions defined
func (s *server) Process(ctx context.Context, request *pb.Request)(*pb.Response, error){

		log.Printf("gRPC backend received: %s", request.Body)

		return &pb.Response{
			Result: fmt.Sprintf("Processed: %s", request.Body),
			Status: 200,
		}, nil
}

func main(){
	listener, error := net.Listen("tcp", ":9091")
	if error != nil {
		log.Fatal("Failed to listen:", error)
	}
	//creates the gRPC server
	grpcServer := grpc.NewServer()
	//hooks my methods to the sewrvr
	pb.RegisterBackendServiceServer(grpcServer, &server{})

	log.Println("gRPC backend listening on :9091")
	if err := grpcServer.Serve(listener); err != nil {
	log.Fatal("Failed to serve:", err)
	}
}