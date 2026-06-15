# Этап 1: Сборка (builder)
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Копируем go.mod и go.sum
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходники
COPY . .

# Собираем бинарник
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

# Этап 2: Финальный образ
FROM alpine:3.18

WORKDIR /app

# Копируем бинарник из builder
COPY --from=builder /app/server .

# Устанавливаем ca-certificates для HTTPS
RUN apk add --no-cache ca-certificates

# Открываем порт
EXPOSE 8080

# Запускаем
CMD ["./server"]
