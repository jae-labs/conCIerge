# ---- Runtime stage ----
FROM alpine:3.22

# ca-certificates: HTTPS API calls; tzdata: timezone support
RUN apk add --no-cache ca-certificates tzdata

# Create a non-root user and group
RUN addgroup -S concierge && adduser -S concierge -G concierge

# TARGETARCH is automatically populated by Buildx (e.g. amd64, arm64)
ARG TARGETARCH
COPY concierge-linux-${TARGETARCH} /concierge

USER concierge

EXPOSE 8080

ENTRYPOINT ["/concierge"]
