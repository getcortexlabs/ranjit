FROM golang:1.27.3-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /ranjit ./cmd/ranjit \
    && mkdir /data && chown 65532:65532 /data

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /ranjit /ranjit
COPY --from=build --chown=65532:65532 /data /data
USER 65532:65532
WORKDIR /data
ENV RANJIT_ADDR=0.0.0.0:8080 RANJIT_DATABASE=/data/ranjit.db
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s CMD ["/ranjit", "healthcheck"]
ENTRYPOINT ["/ranjit"]
