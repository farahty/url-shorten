package scraper

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/farahty/url-shorten/internal/model"
)

type OGScraper struct {
	timeout time.Duration
	maxBody int64
}

func NewOGScraper(timeout time.Duration, maxBody int64) *OGScraper {
	return &OGScraper{timeout: timeout, maxBody: maxBody}
}

func (s *OGScraper) Scrape(ctx context.Context, url string) *model.OGData {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; URLShortener/1.0)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/html") {
		return nil
	}

	body := io.LimitReader(resp.Body, s.maxBody)
	doc, err := goquery.NewDocumentFromReader(body)
	if err != nil {
		return nil
	}

	og := &model.OGData{}
	doc.Find("meta[property]").Each(func(_ int, sel *goquery.Selection) {
		prop, _ := sel.Attr("property")
		content, _ := sel.Attr("content")
		switch prop {
		case "og:title":
			og.Title = content
		case "og:description":
			og.Description = content
		case "og:image":
			og.Image = content
		case "og:site_name":
			og.SiteName = content
		}
	})

	if og.Title == "" && og.Description == "" && og.Image == "" {
		return nil
	}
	return og
}
