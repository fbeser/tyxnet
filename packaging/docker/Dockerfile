FROM --platform=$BUILDPLATFORM golang:1.26.5-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /tyxnet-server ./cmd/tyxnet-server

FROM caddy:2-alpine AS caddy

FROM alpine:3.21
RUN apk add --no-cache ca-certificates iproute2 && mkdir -p /var/lib/tyxnet
ENV TYXNET_CONTAINER=1
COPY --from=build /tyxnet-server /usr/local/bin/tyxnet-server
COPY --from=caddy /usr/bin/caddy /usr/local/bin/caddy
COPY configs/server.docker.yaml /etc/tyxnet/server.yaml
COPY configs/server.https.yaml /etc/tyxnet/server.https.yaml
COPY packaging/docker/caddy-entrypoint.sh /usr/local/bin/tyxnet-caddy-entrypoint
RUN chmod 0555 /usr/local/bin/tyxnet-caddy-entrypoint
# Creating and configuring /dev/net/tun requires root plus NET_ADMIN. The
# Compose example grants only that capability and the TUN device.
EXPOSE 8443/tcp 51830/udp
ENTRYPOINT ["tyxnet-server"]
CMD ["run", "--config", "/etc/tyxnet/server.yaml"]
