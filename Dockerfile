# --- Build Stage ---
# Use the official Go image to build the application.
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum to cache dependencies, which speeds up subsequent builds.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the application as a static binary.
# CGO_ENABLED=0 is crucial for creating a static binary that can run in a minimal image.
# -ldflags="-w -s" strips debug information and symbols, reducing the binary size.
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags="-w -s" -o /kopitiam-run main.go

# --- Final Stage ---
# Use a 'scratch' image, which is an empty image, for the smallest possible footprint.
FROM scratch

# Copy the Certificate Authority certificates from the builder stage.
# This is necessary if your app needs to make outbound HTTPS requests.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the compiled binary from the builder stage.
COPY --from=builder /kopitiam-run /kopitiam-run

# Expose the port the app runs on (as defined in main.go).
EXPOSE 8080

# Command to run the application.
CMD ["/kopitiam-run"]