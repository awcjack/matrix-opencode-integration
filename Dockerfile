FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install git for go mod download
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /matrix-opencode ./cmd/matrix-opencode

# Final stage
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /matrix-opencode /app/matrix-opencode

# Create non-root user
RUN adduser -D -g '' appuser
USER appuser

ENTRYPOINT ["/app/matrix-opencode"]
