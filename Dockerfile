# Builder Stage
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# We name the output 'server' specifically
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags="-w -s" -o /server main.go

# Final Stage
FROM alpine:latest
RUN apk add --no-cache ca-certificates tzdata
# Copy the binary from builder to the root of the final image
COPY --from=builder /server /server

# Expose the port your app listens on
EXPOSE 8080

# Run the binary named 'server'
CMD ["/server"]