package server

import (
	"chirpy/internal/database"
	"chirpy/internal/handlers"
	"database/sql"
	"net/http"
	"os"

	_ "github.com/lib/pq"

	"github.com/joho/godotenv"
)

func StartApp(address string) {

	godotenv.Load()

	dbURL := os.Getenv("DB_URL")

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		os.Exit(1)
	}
	dbQueries := database.New(db)

	serveMux := http.NewServeMux()

	apiConfig := &handlers.ApiConfig{
		DbQueries: dbQueries,
		JwtSecret: os.Getenv("SECRET"),
		PolkaKey:  os.Getenv("POLKA_API_KEY"),
	}

	handlers.RegisterHandlers(serveMux, apiConfig)

	server := http.Server{
		Addr:    address,
		Handler: serveMux,
	}

	server.ListenAndServe()
}
