# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /argocd-destination-api .

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /argocd-destination-api /argocd-destination-api

USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/argocd-destination-api"]
