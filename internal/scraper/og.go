package scraper

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/farahty/url-shorten/internal/model"
)

type OGScraper struct {
	client  *http.Client
	timeout time.Duration
	maxBody int64
}

// isPrivateIP returns true if the IP belongs to a private, loopback, or reserved range.
func isPrivateIP(ip net.IP) bool {
	privateRanges := []net.IPNet{
		{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
		{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)},
		{IP: net.IPv4(192, 168, 0, 0), Mask: net.CIDRMask(16, 32)},
		{IP: net.IPv4(127, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
		{IP: net.IPv4(169, 254, 0, 0), Mask: net.CIDRMask(16, 32)},  // link-local
		{IP: net.IPv4(0, 0, 0, 0), Mask: net.CIDRMask(8, 32)},       // current network
		{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)},    // shared address space (CGN)
		{IP: net.IPv4(192, 0, 0, 0), Mask: net.CIDRMask(24, 32)},     // IETF protocol assignments
		{IP: net.IPv4(198, 18, 0, 0), Mask: net.CIDRMask(15, 32)},    // benchmark testing
		{IP: net.IPv4(224, 0, 0, 0), Mask: net.CIDRMask(4, 32)},      // multicast
		{IP: net.IPv4(240, 0, 0, 0), Mask: net.CIDRMask(4, 32)},      // reserved
	}

	for _, r := range privateRanges {
		if r.Contains(ip) {
			return true
		}
	}

	// Block IPv6 loopback and link-local
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}

	return false
}

func NewOGScraper(timeout time.Duration, maxBody int64) *OGScraper {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: 5 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if isPrivateIP(ip.IP) {
					return nil, errors.New("connections to private/reserved IP ranges are not allowed")
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
		},
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
	return &OGScraper{client: client, timeout: timeout, maxBody: maxBody}
}

func (s *OGScraper) Scrape(ctx context.Context, rawURL string) *model.OGData {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; URLShortener/1.0)")

	resp, err := s.client.Do(req)
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
