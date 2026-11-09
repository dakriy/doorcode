# Builds the doorcode binaries as static executables and packs each into a
# minimal image that runs it as an unprivileged user.
#
#   docker build -t doorcode .                                  # the server
#   docker build --target doorcode-demo -t doorcode-demo .      # the demo
#
# The runtime image is Alpine for its CA certificates (for the identity
# provider and mqtts://) and BusyBox wget, which the compose healthcheck
# uses to ask /healthz.

FROM golang:1.27.3-alpine AS source
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# Each binary builds in a stage of its own, so a target builds only its own.
FROM source AS build-doorcode
RUN go build -ldflags='-s -w' -o /out/doorcode ./cmd/doorcode

FROM source AS build-demo
RUN go build -ldflags='-s -w' -o /out/doorcode-demo ./cmd/doorcode-demo

FROM alpine:3.22 AS runtime
RUN adduser -D -H -u 10001 doorcode
USER 10001:10001
EXPOSE 8080
ENV DOORCODE_LISTEN_ADDR=:8080
HEALTHCHECK --interval=5s --timeout=3s --retries=12 CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

# The demo: the real application over seeded in-memory data and simulated
# doors. Needs neither Postgres nor a broker; nothing outlives it.
FROM runtime AS doorcode-demo
COPY --from=build-demo /out/doorcode-demo /usr/local/bin/doorcode-demo
ENTRYPOINT ["/usr/local/bin/doorcode-demo"]

# The server, the default target.
FROM runtime AS doorcode
COPY --from=build-doorcode /out/doorcode /usr/local/bin/doorcode
ENTRYPOINT ["/usr/local/bin/doorcode"]
