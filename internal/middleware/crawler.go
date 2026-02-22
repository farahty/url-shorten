package middleware

import (
	"context"
	"net/http"
	"strings"
)

var crawlerPatterns = []string{
	"facebookexternalhit",
	"Facebot",
	"Twitterbot",
	"LinkedInBot",
	"Slackbot-LinkExpanding",
	"TelegramBot",
	"Discordbot",
}

func CrawlerDetection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := r.Header.Get("User-Agent")
		isCrawler := false

		for _, pattern := range crawlerPatterns {
			if strings.Contains(ua, pattern) {
				isCrawler = true
				break
			}
		}

		ctx := context.WithValue(r.Context(), IsCrawlerKey, isCrawler)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
