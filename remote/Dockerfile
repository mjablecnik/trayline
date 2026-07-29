FROM golang:1.23.6 AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o server ./cmd/server

FROM gcr.io/distroless/static

COPY --from=build /app/server /server

EXPOSE 8080

ENTRYPOINT ["/server"]
