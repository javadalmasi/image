# مرحله ساخت
FROM golang:1.21-alpine AS builder

# نصب ابزارهای لازم
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# کپی فایل‌های dependency
COPY go.mod go.sum ./
RUN go mod download

# کپی کد منبع
COPY main.go ./

# کامپایل با بهینه‌سازی‌های حداکثری
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -a -installsuffix cgo \
    -o yt-thumbnail-proxy .

# مرحله نهایی - استفاده از scratch برای کوچکترین سایز
FROM scratch

# کپی CA certificates برای HTTPS
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# کپی باینری
COPY --from=builder /app/yt-thumbnail-proxy /yt-thumbnail-proxy

# Expose port
EXPOSE 8080

# اجرای برنامه
ENTRYPOINT ["/yt-thumbnail-proxy"]
