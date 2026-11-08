FROM golang:1.27.3-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /quickstart ./examples/quickstart \
    && CGO_ENABLED=0 go build -trimpath -o /backend ./examples/backend \
    && CGO_ENABLED=0 go build -trimpath -o /receiver ./examples/webhook \
    && mkdir /data && chown 65532:65532 /data

FROM scratch AS tools
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /quickstart /quickstart
COPY --from=build --chown=65532:65532 /data /data
USER 65532:65532
ENTRYPOINT ["/quickstart"]

FROM tools AS backend
COPY --from=build /backend /backend
ENTRYPOINT ["/backend"]

FROM tools AS receiver
COPY --from=build /receiver /receiver
WORKDIR /data
ENTRYPOINT ["/receiver"]
