# syntax=docker/dockerfile:1
ARG GO_VERSION=1.26.10
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/benfolio-server ./cmd/benfolio-server
RUN mkdir -p /out/data && chmod 700 /out/data
RUN mkdir -p /out/licenses/dependencies \
    && cp /usr/local/go/LICENSE /out/licenses/Go-LICENSE \
    && cp /src/LICENSE /out/licenses/Benfolio-Server-LICENSE \
    && cp -R /src/third_party /out/licenses/third_party \
    && cd /go/pkg/mod \
    && find . -type f \( -iname 'license*' -o -iname 'licence*' \
        -o -iname 'copying*' -o -iname 'notice*' -o -iname 'copyright*' \) \
        -exec cp --parents '{}' /out/licenses/dependencies/ \;

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/benfolio-server /benfolio-server
COPY --from=build /out/licenses /licenses
COPY --from=build --chown=10001:10001 /out/data /data
USER 10001:10001
ENV BENFOLIO_LISTEN_ADDR=:8080 BENFOLIO_DATA_DIR=/data BENFOLIO_BOOKS_DIR=/books
EXPOSE 8080
STOPSIGNAL SIGTERM
ENTRYPOINT ["/benfolio-server"]
