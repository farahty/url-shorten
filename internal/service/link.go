package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/farahty/url-shorten/internal/cache"
	"github.com/farahty/url-shorten/internal/config"
	"github.com/farahty/url-shorten/internal/model"
	"github.com/farahty/url-shorten/internal/repository"
	"github.com/farahty/url-shorten/internal/scraper"
	"github.com/farahty/url-shorten/internal/shortcode"
	"github.com/redis/go-redis/v9"
)

var (
	ErrInvalidURL    = errors.New("invalid URL")
	ErrAliasConflict = errors.New("alias already taken")
	ErrLinkExpired   = errors.New("link has expired")
)

type LinkService struct {
	repo    *repository.LinkRepository
	cache   *cache.RedisCache
	scraper *scraper.OGScraper
	cfg     *config.Config
	clickCh chan string
}

func NewLinkService(
	repo *repository.LinkRepository,
	cache *cache.RedisCache,
	scraper *scraper.OGScraper,
	cfg *config.Config,
) *LinkService {
	s := &LinkService{
		repo:    repo,
		cache:   cache,
		scraper: scraper,
		cfg:     cfg,
		clickCh: make(chan string, cfg.ClickBufferSize),
	}
	go s.clickFlusher()
	return s
}

func (s *LinkService) Create(ctx context.Context, req model.CreateLinkRequest, apiKeyID string) (*model.Link, error) {
	if _, err := url.ParseRequestURI(req.URL); err != nil {
		return nil, ErrInvalidURL
	}
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		return nil, ErrInvalidURL
	}

	id, err := s.repo.NextID(ctx)
	if err != nil {
		return nil, fmt.Errorf("generating ID: %w", err)
	}

	code := shortcode.Encode(id)
	isAlias := false

	if req.Alias != "" {
		exists, err := s.repo.CodeExists(ctx, req.Alias)
		if err != nil {
			return nil, fmt.Errorf("checking alias: %w", err)
		}
		if exists {
			return nil, ErrAliasConflict
		}
		code = req.Alias
		isAlias = true
	}

	link := &model.Link{
		ID:          id,
		Code:        code,
		OriginalURL: req.URL,
		IsAlias:     isAlias,
		APIKeyID:    apiKeyID,
	}

	if req.ExpiresIn != nil && *req.ExpiresIn > 0 {
		exp := time.Now().Add(time.Duration(*req.ExpiresIn) * time.Second)
		link.ExpiresAt = &exp
	}

	// Scrape OG metadata
	if og := s.scraper.Scrape(ctx, req.URL); og != nil {
		if og.Title != "" {
			link.OGTitle = &og.Title
		}
		if og.Description != "" {
			link.OGDesc = &og.Description
		}
		if og.Image != "" {
			link.OGImage = &og.Image
		}
		if og.SiteName != "" {
			link.OGSite = &og.SiteName
		}
	}

	if err := s.repo.Create(ctx, link); err != nil {
		return nil, fmt.Errorf("creating link: %w", err)
	}

	link.CreatedAt = time.Now()
	return link, nil
}

func (s *LinkService) GetByCode(ctx context.Context, code string) (*model.Link, error) {
	link, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if link.ExpiresAt != nil && link.ExpiresAt.Before(time.Now()) {
		return nil, ErrLinkExpired
	}
	return link, nil
}

func (s *LinkService) List(ctx context.Context, apiKeyID string, page, limit int) ([]model.Link, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return s.repo.List(ctx, apiKeyID, page, limit)
}

func (s *LinkService) Delete(ctx context.Context, code, apiKeyID string) error {
	if err := s.repo.Delete(ctx, code, apiKeyID); err != nil {
		return err
	}
	_ = s.cache.Delete(ctx, code)
	return nil
}

func (s *LinkService) Resolve(ctx context.Context, code string) (string, error) {
	// Try cache first
	if url, err := s.cache.Get(ctx, code); err == nil {
		s.clickCh <- code
		return url, nil
	} else if !errors.Is(err, redis.Nil) {
		log.Printf("redis cache error: %v", err)
	}

	// Fall back to database
	link, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return "", err
	}

	if link.ExpiresAt != nil && link.ExpiresAt.Before(time.Now()) {
		return "", ErrLinkExpired
	}

	// Cache for future lookups
	_ = s.cache.Set(ctx, code, link.OriginalURL)

	s.clickCh <- code
	return link.OriginalURL, nil
}

func (s *LinkService) ResolveForCrawler(ctx context.Context, code string) (*model.Link, error) {
	link, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if link.ExpiresAt != nil && link.ExpiresAt.Before(time.Now()) {
		return nil, ErrLinkExpired
	}
	s.clickCh <- code
	return link, nil
}

func (s *LinkService) clickFlusher() {
	ticker := time.NewTicker(s.cfg.ClickFlushInterval)
	defer ticker.Stop()

	var buffer []string

	for {
		select {
		case code := <-s.clickCh:
			buffer = append(buffer, code)
			if len(buffer) >= s.cfg.ClickBufferSize {
				s.flushClicks(buffer)
				buffer = nil
			}
		case <-ticker.C:
			if len(buffer) > 0 {
				s.flushClicks(buffer)
				buffer = nil
			}
		}
	}
}

func (s *LinkService) flushClicks(codes []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.repo.IncrementClickCount(ctx, codes); err != nil {
		log.Printf("error flushing click counts: %v", err)
	}
}
