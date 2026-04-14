FROM golang:1.24 AS builder

#sets the working directory inside the container to /app
WORKDIR /app 

#copies the go.mod file from the current directory on the host machine to the /app directory in the container
COPY go.mod ./
RUN go mod download 

#copy everything form local direcotry into container
COPY . .

#docker build step that converts code into binary in the output file 
RUN CGO_ENABLED=0 GOOS=linux go build -o grpc_backend_output ./grpc_backend

#starts at a tiny Linux image 
FROM alpine:latest

#sets working direcotry inside that container 
WORKDIR /app

#copies the compiled proxy binary from the builder stage to the current stage.
COPY --from=builder /app/grpc_backend_output .

#opens the port in this container 
EXPOSE 9091

#this is the copmmand it runs when the container starts 
CMD ["./grpc_backend_output"]