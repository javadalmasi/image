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

	"github.com/chai2010/webp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // Import for WebP decoding support
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

// تابع بررسی محدودیت ابعاد مجاز
func isValidDimension(width, height int) bool {
	// ابعاد مجاز
	validDimensions := [][2]int{
		{426, 240},
		{640, 360},
		{854, 480},
		{960, 540},
		{1024, 576},
		{1280, 720},
		{1600, 900},
		{1920, 1080},
	}
	
	for _, dim := range validDimensions {
		if width == dim[0] && height == dim[1] {
			return true
		}
	}
	return false
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	// استخراج Video ID
	matches := videoIDRegex.FindStringSubmatch(r.URL.Path)
	if matches == nil || len(matches) < 2 {
		http.Error(w, "Invalid video ID format", http.StatusBadRequest)
		return
	}

	videoID := matches[1]

	// خواندن پارامترهای resize و quality و format از query string
	resizeParam := r.URL.Query().Get("resize")
	qualityParam := r.URL.Query().Get("quality")
	formatParam := r.URL.Query().Get("format")

	var targetWidth, targetHeight uint
	var targetQuality int
	var targetFormat string
	hasResize := false
	hasQuality := false
	hasFormat := false

	// پردازش پارامتر resize
	if resizeParam != "" {
		// جدا کردن عرض و ارتفاع
		var width, height int
		n, err := fmt.Sscanf(resizeParam, "%d,%d", &width, &height)
		if err == nil && n == 2 {
			// بررسی محدودیت ابعاد مجاز
			if isValidDimension(width, height) {
				targetWidth = uint(width)
				targetHeight = uint(height)
				hasResize = true
			} else {
				// اگر ابعاد خارج از محدوده مجاز باشد، خطایی نمایش داده می‌شود
				http.Error(w, "Invalid resize dimensions", http.StatusBadRequest)
				return
			}
		} else {
			// فرمت اشتباه
			http.Error(w, "Invalid resize format. Expected format: ?resize=width,height", http.StatusBadRequest)
			return
		}
	}

	// پردازش پارامتر quality
	if qualityParam != "" {
		quality, err := strconv.Atoi(qualityParam)
		if err != nil {
			// اگر مقدار quality عدد نباشد، خطایی نمایش داده می‌شود
			http.Error(w, "Quality parameter must be a number", http.StatusBadRequest)
			return
		}
		// بررسی اینکه مقدار باید 75 یا 85 باشد
		if quality != 75 && quality != 85 {
			http.Error(w, "Quality parameter not accepted", http.StatusBadRequest)
			return
		}
		targetQuality = quality
		hasQuality = true
	}

	// پردازش پارامتر format
	if formatParam != "" {
		// فرمت‌های webp و jpeg پشتیبانی می‌شوند (avif در آینده پیاده‌سازی خواهد شد)
		if formatParam == "webp" {
			targetFormat = formatParam
			hasFormat = true
		} else if formatParam == "jpeg" || formatParam == "jpg" {
			// jpeg را نیز می‌پذیرد، اما مقدار داخل targetFormat همچنان jpeg خواهد بود
			targetFormat = "jpeg"
			hasFormat = true
		} else if formatParam == "avif" {
			// AVIF در حال حاضر پشتیبانی نمی‌شود و فقط برای نشان دادن که قرار است پیاده‌سازی شود اضافه شده است
			// در آینده می‌توان یک کتابخانه سوم‌شخص مانند github.com/Kagami/go-avif اضافه کرد
			http.Error(w, "Format parameter not accepted. AVIF support coming soon", http.StatusBadRequest)
			return
		} else {
			http.Error(w, "Format parameter not accepted. Only webp and jpeg are supported", http.StatusBadRequest)
			return
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
				// اگر نیازی به تغییر اندازه یا کیفیت نیست، اما فرمت درخواست داده شده است
				if !hasResize && !hasQuality {
					// اگر فرمت خاصی درخواست شده باشد، تصویر را تغییر فرمت دهیم
					if hasFormat && targetFormat == "webp" {
						// خواندن تصویر از پاسخ
						img, _, err := image.Decode(resp.Body)
						resp.Body.Close()
						if err != nil {
							continue
						}
						
						// تنظیم کیفیت پیش‌فرض برای WebP
						webpQuality := float32(85)

						// تنظیم هدرهای بهینه برای WebP
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
						w.Header().Set("Content-Type", "image/webp")
						w.Header().Set("X-Content-Type-Options", "nosniff")
						
						// کد کردن تصویر به WebP
						w.WriteHeader(http.StatusOK)
						encodeErr := webp.Encode(w, img, &webp.Options{Lossless: false, Quality: webpQuality})
						if encodeErr != nil {
							http.Error(w, "Error encoding image to WebP", http.StatusInternalServerError)
							return
						}
						return
					} else {
						// تنظیم هدرهای بهینه برای JPEG یا فرمت پیش‌فرض
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
						if hasFormat && targetFormat == "jpeg" {
							w.Header().Set("Content-Type", "image/jpeg")
						} else {
							// فرمت پیش‌فرض
							w.Header().Set("Content-Type", "image/jpeg")
						}
						w.Header().Set("X-Content-Type-Options", "nosniff")
						
						// کپی مستقیم بدون بافر اضافی
						w.WriteHeader(http.StatusOK)
						_, _ = io.Copy(w, resp.Body)
						resp.Body.Close()
						return
					}
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
					// استفاده از الگوریتم CatmullRom برای تغییر اندازه
					draw.CatmullRom.Scale(m, m.Bounds(), img, img.Bounds(), draw.Src, nil)
					img = m
				}

				// تعیین فرمت خروجی و کیفیت
				if hasFormat && targetFormat == "webp" {
					// تنظیم کیفیت برای WebP
					var webpQuality float32
					if hasQuality {
						// استفاده از کیفیت JPEG (75, 85) به عنوان مقیاس WebP
						webpQuality = float32(targetQuality)
					} else {
						webpQuality = 85 // کیفیت پیش‌فرض
					}

					// تنظیم هدرهای بهینه برای WebP
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					w.Header().Set("Content-Type", "image/webp")
					w.Header().Set("X-Content-Type-Options", "nosniff")
					
					// کد کردن تصویر پردازش شده به WebP
					w.WriteHeader(http.StatusOK)
					err := webp.Encode(w, img, &webp.Options{Lossless: false, Quality: webpQuality})
					if err != nil {
						http.Error(w, "Error encoding image to WebP", http.StatusInternalServerError)
						return
					}
				} else {
					// تنظیم کیفیت تصویر برای JPEG
					var opts *jpeg.Options
					if hasQuality {
						opts = &jpeg.Options{Quality: targetQuality}
					} else {
						opts = &jpeg.Options{Quality: 90} // کیفیت پیش‌فرض
					}

					// تنظیم هدرهای بهینه برای JPEG
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					w.Header().Set("Content-Type", "image/jpeg")
					w.Header().Set("X-Content-Type-Options", "nosniff")
					
					// کد کردن تصویر پردازش شده به JPEG
					w.WriteHeader(http.StatusOK)
					err := jpeg.Encode(w, img, opts)
					if err != nil {
						http.Error(w, "Error encoding image to JPEG", http.StatusInternalServerError)
						return
					}
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