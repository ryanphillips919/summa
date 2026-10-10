
FROM golang:1.27 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o summa .

FROM debian:bookworm-slim

WORKDIR /app
COPY --from=builder /app/summa .

EXPOSE 8080

CMD ["./summa"]
