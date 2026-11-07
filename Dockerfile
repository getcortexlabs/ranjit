FROM --platform=$BUILDPLATFORM golang:1.27.3-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /ranjit ./cmd/ranjit \
    && mkdir /data && chown 65532:65532 /data

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /ranjit /ranjit
COPY --from=build --chown=65532:65532 /data /data
USER 65532:65532
WORKDIR /data
ENV RANJIT_ADDR=0.0.0.0:8080 RANJIT_DATABASE=/data/ranjit.db
ENV XDG_CONFIG_HOME=/data
LABEL org.opencontainers.image.source="https://github.com/getcortexlabs/ranjit" \
      org.opencontainers.image.licenses="MIT"
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s CMD ["/ranjit", "healthcheck"]
ENTRYPOINT ["/ranjit"]
CMD ["serve"]
