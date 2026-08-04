FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tyxnet-server ./cmd/tyxnet-server

FROM alpine:3.21
RUN addgroup -S tyxnet && adduser -S -G tyxnet tyxnet && mkdir -p /var/lib/tyxnet && chown tyxnet:tyxnet /var/lib/tyxnet
COPY --from=build /tyxnet-server /usr/local/bin/tyxnet-server
COPY configs/server.yaml /etc/tyxnet/server.yaml
USER tyxnet
EXPOSE 8443/tcp 51830/udp
ENTRYPOINT ["tyxnet-server"]
CMD ["run", "--config", "/etc/tyxnet/server.yaml"]
