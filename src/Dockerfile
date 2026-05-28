# ---- Builder stage ----
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy dependency manifests first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy remaining source
COPY . .

# Build a static binary with stripped debug symbols
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /concierge ./cmd/concierge

# ---- Runtime stage ----
FROM alpine:3.22

# ca-certificates: HTTPS API calls; tzdata: timezone support
RUN apk add --no-cache ca-certificates tzdata

# Create a non-root user and group
RUN addgroup -S concierge && adduser -S concierge -G concierge

COPY --from=builder /concierge /concierge

USER concierge

EXPOSE 8080

ENTRYPOINT ["/concierge"]
