package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/farahty/url-shorten/internal/model"
)

// panickingScraper implements the minimal surface LinkService uses in the
// background goroutine. We assert the process survives its panic.
type panickingScraper struct{ called chan struct{} }

func (p *panickingScraper) Scrape(ctx context.Context, raw string) *model.OGData {
	close(p.called)
	panic("boom")
}

func TestScrapeGoroutinePanicDoesNotCrashProcess(t *testing.T) {
	ps := &panickingScraper{called: make(chan struct{})}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runScrapeJob(context.Background(), ps, nil, "abc", "https://example.com")
	}()

	select {
	case <-ps.called:
	case <-time.After(time.Second):
		t.Fatal("scraper was never invoked")
	}
	wg.Wait() // returns only if runScrapeJob recovered
}
