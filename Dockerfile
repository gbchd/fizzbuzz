FROM golang:1.26.5-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/fizzbuzz ./cmd/fizzbuzz

FROM gcr.io/distroless/static-debian12:nonroot AS runtime

COPY --from=builder /out/fizzbuzz /fizzbuzz

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/fizzbuzz"]
