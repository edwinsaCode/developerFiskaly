# =====================================================
# Stage: base — Go runtime + tools
# =====================================================
# Versi Go WAJIB mengikuti `go` di go.mod. GOTOOLCHAIN=local di image ini
# berarti toolchain tidak akan diunduh otomatis: image yang tertinggal membuat
# container gagal boot berulang ("go.mod requires go >= …") sementara `make
# start` tetap melapor "Started".
FROM golang:1.25-alpine AS base
WORKDIR /app
RUN apk --no-cache add ca-certificates tzdata git curl

# =====================================================
# Stage: dev — hot-reload dengan Air
# Source di-mount sebagai volume; go mod tidy dijalankan
# saat container start untuk generate/update go.sum.
# =====================================================
FROM base AS dev
RUN go install github.com/air-verse/air@v1.52.3 && \
    go install -tags 'mysql' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.17.0
EXPOSE 8080
# go mod tidy dijalankan pertama untuk memastikan go.sum tersedia
CMD ["sh", "-c", "go mod tidy && air -c .air.toml"]

# =====================================================
# Stage: builder — compile binary production
# Membutuhkan go.sum yang sudah di-commit ke repo.
# =====================================================
FROM base AS builder
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /app/server ./cmd/api

# =====================================================
# Stage: prod — image minimal berisi binary + migrate
# =====================================================
FROM alpine:3.19 AS prod
WORKDIR /app
RUN apk --no-cache add ca-certificates tzdata curl && \
    curl -sSL https://github.com/golang-migrate/migrate/releases/download/v4.17.0/migrate.linux-amd64.tar.gz \
    | tar -xz -C /usr/local/bin migrate
COPY --from=builder /app/server .
COPY migrations ./migrations
EXPOSE 8080
CMD ["./server"]
