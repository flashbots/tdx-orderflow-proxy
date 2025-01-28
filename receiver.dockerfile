FROM golang:1.24rc2-bullseye@sha256:236da40764c1bcf469fcaf6ca225ca881c3f06cbd1934e392d6e4af3484f6cac AS builder
WORKDIR /app
COPY . /app
RUN make build-receiver-proxy

FROM gcr.io/distroless/cc-debian12:nonroot-6755e21ccd99ddead6edc8106ba03888cbeed41a
WORKDIR /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/build/receiver-proxy /app/receiver-proxy
ENV LISTEN_ADDR=":8080"
EXPOSE 8080
ENTRYPOINT ["/app/receiver-proxy"]
