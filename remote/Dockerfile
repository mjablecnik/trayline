FROM golang:1.23.6 AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o server ./cmd/server

FROM debian:bookworm-20240722-slim

RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /app/server /server

EXPOSE 8080

ENTRYPOINT ["/server"]
