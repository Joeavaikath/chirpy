package main

import (
	"chirpy/internal/database"
	"chirpy/internal/handlers"
	"database/sql"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func main() {

	godotenv.Load()

	dbURL := os.Getenv("DB_URL")
	jwtSecret := os.Getenv("SECRET")
	polkaKey := os.Getenv("POLKA_API_KEY")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		os.Exit(1)
	}

	dbQueries := database.New(db)

	serveMux := http.NewServeMux()

	apiConfig := &handlers.ApiConfig{
		DbQueries: dbQueries,
		JwtSecret: jwtSecret,
		PolkaKey:  polkaKey,
	}

	handlers.RegisterUserRoutes(serveMux, apiConfig)
	handlers.RegisterChirpRoutes(serveMux, apiConfig)
	handlers.RegisterAdminRoutes(serveMux, apiConfig)
	handlers.RegisterTokenRoutes(serveMux, apiConfig)
	handlers.RegisterWebhookRoutes(serveMux, apiConfig)
	handlers.RegisterMetricsRoutes(serveMux, apiConfig)

	server := http.Server{
		Addr:    ":8080",
		Handler: serveMux,
	}

	server.ListenAndServe()
}
