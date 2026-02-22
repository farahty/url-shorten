package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/farahty/url-shorten/internal/cache"
	"github.com/farahty/url-shorten/internal/config"
	"github.com/farahty/url-shorten/internal/handler"
	"github.com/farahty/url-shorten/internal/middleware"
	"github.com/farahty/url-shorten/internal/repository"
	"github.com/farahty/url-shorten/internal/scraper"
	"github.com/farahty/url-shorten/internal/service"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

func main() {
	_ = godotenv.Load()

	cfg := config.Load()
	ctx := context.Background()

	// PostgreSQL
	dbPool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer dbPool.Close()

	if err := dbPool.Ping(ctx); err != nil {
		log.Fatalf("failed to ping database: %v", err)
	}
	log.Println("connected to PostgreSQL")

	// Redis
	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		log.Fatalf("failed to parse redis URL: %v", err)
	}
	redisClient := redis.NewClient(redisOpts)
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to connect to redis: %v", err)
	}
	defer redisClient.Close()
	log.Println("connected to Redis")

	// Layers
	repo := repository.NewLinkRepository(dbPool)
	redisCache := cache.NewRedisCache(redisClient, cfg.RedisCacheTTL)
	ogScraper := scraper.NewOGScraper(cfg.OGScrapeTimeout, cfg.OGScrapeMaxBody)
	linkService := service.NewLinkService(repo, redisCache, ogScraper, cfg)

	// Handlers
	linkHandler := handler.NewLinkHandler(linkService, cfg)
	redirectHandler := handler.NewRedirectHandler(linkService, cfg)
	qrHandler := handler.NewQRHandler(linkService, cfg)

	// Router
	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.RealIP)

	// API routes (authenticated)
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.APIKeyAuth(repo))

		r.Post("/links", linkHandler.Create)
		r.Get("/links", linkHandler.List)
		r.Get("/links/{code}", linkHandler.Get)
		r.Delete("/links/{code}", linkHandler.Delete)
		r.Get("/links/{code}/qr", qrHandler.GetQR)
	})

	// Public redirect route
	r.With(middleware.CrawlerDetection).Get("/{code}", redirectHandler.Redirect)

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("server starting on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}
	log.Println("server stopped")
}
