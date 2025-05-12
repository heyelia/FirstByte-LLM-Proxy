# Stage 1: Build the Go application
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum files to download dependencies
COPY go.mod ./
# If you had a go.sum (after go mod tidy or go get), you'd copy it too:
# COPY go.sum ./

# Download dependencies (not strictly necessary if no external deps beyond standard library)
# RUN go mod download

# Copy the source code
COPY . .

# Build the application
# CGO_ENABLED=0 for a statically linked binary, GOOS=linux for cross-compilation if needed
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o openai-go-proxy .

# Stage 2: Create the final lightweight image
FROM alpine:latest

WORKDIR /root/

# Copy the built binary from the builder stage
COPY --from=builder /app/openai-go-proxy .

# Expose the port the app runs on
EXPOSE 8081

# Set environment variables (can be overridden in compose.yml)
ENV PORT=8081
# OPENAI_API_KEY should be set in compose.yml or via other means, not hardcoded here

# Command to run the application
CMD ["./openai-go-proxy"]
