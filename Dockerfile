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

# نصب ابزارهای لازم برای کامپایل CGO
RUN apk add --no-cache gcc musl-dev

# کامپایل با بهینه‌سازی‌های حداکثری - استفاده از معماری پیش‌فرض داکر
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-w -s" \
    -a -installsuffix cgo \
    -o yt-thumbnail-proxy .

# مرحله نهایی - استفاده از alpine برای پشتیبانی از CGO
FROM alpine:latest

# نصب CA certificates با غیرفعال کردن triggers برای جلوگیری از مشکلات ایمیولیشن
RUN apk add --no-cache --no-scripts ca-certificates

WORKDIR /root/

# کپی باینری
COPY --from=builder /app/yt-thumbnail-proxy /yt-thumbnail-proxy

# Expose port
EXPOSE 8080

# اجرای برنامه
ENTRYPOINT ["./yt-thumbnail-proxy"]