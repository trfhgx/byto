FROM golang:1.22 AS builder
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/llm-gateway ./cmd/gateway

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/llm-gateway /app/llm-gateway
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/llm-gateway"]
