# ---- Runtime deps stage, has apk ----
FROM dhi.io/alpine-base:3.23-dev AS runtime-deps

RUN apk add --no-cache ca-certificates tzdata

# ---- Final runtime, no apk ----
FROM dhi.io/alpine-base:3.23

COPY --from=runtime-deps /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=runtime-deps /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=runtime-deps /etc/ssl/cert.pem /etc/ssl/cert.pem

ARG TARGETARCH
COPY concierge-linux-${TARGETARCH} /concierge

EXPOSE 8080

ENTRYPOINT ["/concierge"]
