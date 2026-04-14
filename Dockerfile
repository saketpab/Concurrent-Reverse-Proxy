# Stage 1: Build
#starts a new Docker image 
FROM golang:1.24 AS builder 

#sets the workiing directory inside the container to /app
WORKDIR /app

#copies the go.mod file from the current directory on the host machine to the /app directory in the container
#go.mod is for dependency management in Go projects (for dependcies outside of Go that you download), 
#it lists the dependencies required for the project and their versions
COPY go.mod ./
#downloads the dependencies in go.mod file from the internet 
RUN go mod download

#copy everything from this local directory into the working directory inside the Docker image 
COPY . .

#RUN: tells docker to execute this command while building the image
#CGO_ENABLED=0: diaables Go's ability to call C code so that this is a full finary file that doesn't depend on system libraries
#GOOS=linux: sets the target operating system to linux, which is necessary because the final image will be based on alpine, which is a linux distribution
#go build -o proxy .: compiles the Go code in the current directory and outputs
RUN CGO_ENABLED=0 GOOS=linux go build -o proxy .

# Stage 2: Run
#👉 “Use a minimal Alpine Linux environment as the starting point for this container.”
FROM alpine:latest

#sets the working directory inside the container to /app
WORKDIR /app

#copies the compiled proxy binary from the builder stage to the current stage.
COPY --from=builder /app/proxy .

#tells Docker that the container listens on the specified network port at runtime.
EXPOSE 8080

#specifies the command to run when the container starts. In this case, it runs the compiled proxy binary.
CMD ["./proxy"]