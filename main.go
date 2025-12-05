package main

import (
	"fmt"
	"io"
	"image"
	"image/jpeg"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/net/http2"
)

var (
	videoIDRegex = regexp.MustCompile(`/vi/([a-zA-Z0-9_-]{11})$`)
	
	// لیست هاست‌های مختلف YouTube برای توزیع بار
	ytHosts = []string{
		"i.ytimg.com",
		"i1.ytimg.com",
		"i2.ytimg.com",
		"i3.ytimg.com",
		"i4.ytimg.com",
		"i9.ytimg.com",
	}
	
	// HTTP client با HTTP/2 و connection pooling
	client *http.Client
)

func init() {
	// تنظیم HTTP client با HTTP/2 و پارامترهای بهینه
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  false,
		ForceAttemptHTTP2:   true,
	}
	
	// فعال‌سازی HTTP/2
	http2.ConfigureTransport(transport)
	
	client = &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}
}

// تولید URL های تصویر با چرخش بین هاست‌ها
func generateImageURLs(videoID string, hostIndex int) []string {
	host := ytHosts[hostIndex%len(ytHosts)]
	
	return []string{
		fmt.Sprintf("https://%s/vi/%s/maxresdefault.jpg", host, videoID),
		fmt.Sprintf("https://%s/vi/%s/sddefault.jpg", host, videoID),
		fmt.Sprintf("https://%s/vi/%s/hqdefault.jpg", host, videoID),
		fmt.Sprintf("https://%s/vi/%s/mqdefault.jpg", host, videoID),
		fmt.Sprintf("https://%s/vi/%s/default.jpg", host, videoID),
	}
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	// استخراج Video ID
	matches := videoIDRegex.FindStringSubmatch(r.URL.Path)
	if matches == nil || len(matches) < 2 {
		http.Error(w, "Invalid video ID format", http.StatusBadRequest)
		return
	}

	videoID := matches[1]

	// خواندن پارامترهای resize و quality از query string
	resizeParam := r.URL.Query().Get("resize")
	qualityParam := r.URL.Query().Get("quality")

	var targetWidth, targetHeight uint
	var targetQuality int
	hasResize := false
	hasQuality := false

	// پردازش پارامتر resize
	if resizeParam != "" {
		// جدا کردن عرض و ارتفاع
		var width, height int
		n, err := fmt.Sscanf(resizeParam, "%d,%d", &width, &height)
		if err == nil && n == 2 {
			if width > 0 && height > 0 {
				targetWidth = uint(width)
				targetHeight = uint(height)
				hasResize = true
			}
		}
	}

	// پردازش پارامتر quality
	if qualityParam != "" {
		quality, err := strconv.Atoi(qualityParam)
		if err == nil && quality >= 1 && quality <= 100 {
			targetQuality = quality
			hasQuality = true
		}
	}

	// تلاش برای دریافت تصویر از هاست‌های مختلف
	for hostIndex := 0; hostIndex < len(ytHosts); hostIndex++ {
		imageURLs := generateImageURLs(videoID, hostIndex)
		
		for _, imageURL := range imageURLs {
			resp, err := client.Get(imageURL)
			if err != nil {
				continue
			}
			
			if resp.StatusCode == http.StatusOK {
				// اگر نیازی به تغییر اندازه یا کیفیت نیست، تصویر اصلی را برگردان
				if !hasResize && !hasQuality {
					// تنظیم هدرهای بهینه
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					w.Header().Set("Content-Type", "image/jpeg")
					w.Header().Set("X-Content-Type-Options", "nosniff")
					
					// کپی مستقیم بدون بافر اضافی
					w.WriteHeader(http.StatusOK)
					_, _ = io.Copy(w, resp.Body)
					resp.Body.Close()
					return
				}

				// خواندن تصویر برای پردازش
				img, _, err := image.Decode(resp.Body)
				resp.Body.Close()
				if err != nil {
					continue
				}

				// تغییر اندازه تصویر در صورت نیاز
				if hasResize {
					// ایجاد یک تصویر جدید با ابعاد مورد نظر
					m := image.NewRGBA(image.Rect(0, 0, int(targetWidth), int(targetHeight)))
					// استفاده از الگوریتم Lanczos برای تغییر اندازه
					draw.CatmullRom.Scale(m, m.Bounds(), img, img.Bounds(), draw.Src, nil)
					img = m
				}

				// تنظیم کیفیت تصویر
				var opts *jpeg.Options
				if hasQuality {
					opts = &jpeg.Options{Quality: targetQuality}
				} else {
					opts = &jpeg.Options{Quality: 90} // کیفیت پیش‌فرض
				}

				// تنظیم هدرهای بهینه
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				w.Header().Set("Content-Type", "image/jpeg")
				w.Header().Set("X-Content-Type-Options", "nosniff")
				
				// کد کردن تصویر پردازش شده
				w.WriteHeader(http.StatusOK)
				err = jpeg.Encode(w, img, opts)
				if err != nil {
					http.Error(w, "Error encoding image", http.StatusInternalServerError)
					return
				}
				return
			}
			resp.Body.Close()
		}
	}

	// تصویر پیدا نشد - پاسخ 404
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("Thumbnail not found"))
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func main() {
	http.HandleFunc("/vi/", handleRequest)
	http.HandleFunc("/health", healthCheck)
	
	port := ":8080"
	log.Printf("Server starting on port %s with HTTP/2 support", port)
	
	// استفاده از HTTP/2
	server := &http.Server{
		Addr:         port,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	
	log.Fatal(server.ListenAndServe())
}