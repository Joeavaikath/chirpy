package main

import (
	"chirpy/internal/database"
	"database/sql"
	"encoding/json"
	"html/template"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	dbQueries      *database.Queries
}

func main() {

	godotenv.Load()

	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		os.Exit(1)
	}

	dbQueries := database.New(db)

	serveMux := http.NewServeMux()

	apiConfig := &apiConfig{
		dbQueries: dbQueries,
	}

	serveMux.Handle("/app/", apiConfig.middlewareMetricsInc(http.StripPrefix("/app", http.FileServer(http.Dir(".")))))
	serveMux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	serveMux.Handle("GET /admin/metrics", http.HandlerFunc(apiConfig.printMetric))
	serveMux.Handle("POST /admin/reset", http.HandlerFunc(apiConfig.resetMetric))

	serveMux.Handle("POST /api/validate_chirp", http.HandlerFunc(validateChirp))
	serveMux.Handle("POST /api/users", http.HandlerFunc(apiConfig.createUser))

	server := http.Server{
		Addr:    ":8080",
		Handler: serveMux,
	}

	server.ListenAndServe()
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

type MetricPageData struct {
	Hits int32
}

func (cfg *apiConfig) printMetric(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("./metrics/index.html"))
	data := MetricPageData{
		Hits: cfg.fileserverHits.Load(),
	}
	tmpl.Execute(w, data)
}

func (cfg *apiConfig) resetMetric(w http.ResponseWriter, r *http.Request) {
	cfg.fileserverHits.Store(0)
	if os.Getenv("PLATFORM") != "dev" {
		invalid := invalidChirp{}
		respondWithError(w, 403, invalid)
	}
	cfg.dbQueries.DropUsers(r.Context())
}

type chirp struct {
	Body string `json:"body"`
}

type validChirp struct {
	Valid bool `json:"valid"`
}

type cleanedChirp struct {
	CleanChirp string `json:"cleaned_body"`
}

type invalidChirp struct {
	Error string `json:"error"`
}

type emailRequest struct {
	Email string `json:"email"`
}

type User struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
}

func validateChirp(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	decoder := json.NewDecoder(r.Body)
	params := chirp{}
	err := decoder.Decode(&params)

	if err != nil {
		invalid := invalidChirp{
			Error: "Something went wrong",
		}
		respondWithError(w, 500, invalid)
		return
	}

	if len(params.Body) > 140 {
		invalid := invalidChirp{
			Error: "Chirp is too long",
		}
		respondWithError(w, 400, invalid)
		return
	}

	profaneList := []string{"kerfuffle", "sharbert", "fornax"}
	cleanedBody := replaceProfane(params.Body, profaneList)

	cleanChirp := cleanedChirp{
		CleanChirp: cleanedBody,
	}

	respondWithJSON(w, 200, cleanChirp)
}

func replaceProfane(message string, profaneList []string) string {
	words := strings.Split(message, " ")
	cleanedWords := []string{}
	for _, word := range words {
		if sliceContains(profaneList, strings.ToLower(word)) {
			cleanedWords = append(cleanedWords, "****")
		} else {
			cleanedWords = append(cleanedWords, word)
		}
	}
	return strings.Join(cleanedWords, " ")
}

func respondWithError(w http.ResponseWriter, code int, errorPayload interface{}) {
	w.WriteHeader(code)
	dat, _ := json.Marshal(errorPayload)
	w.Write(dat)
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.WriteHeader(code)
	dat, _ := json.Marshal(payload)
	w.Write(dat)
}

func sliceContains[T comparable](slice []T, item T) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

func (cfg *apiConfig) createUser(w http.ResponseWriter, r *http.Request) {
	params, err := decodeJSON[emailRequest](r)
	if err != nil {
		invalid := invalidChirp{
			Error: "Something went wrong",
		}
		respondWithError(w, 500, invalid)
	}
	user, err := cfg.dbQueries.CreateUser(r.Context(), params.Email)

	if err != nil {
		invalid := invalidChirp{
			Error: "Something went wrong",
		}
		respondWithError(w, 500, invalid)
	}
	userResponse := User{
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     params.Email,
	}

	respondWithJSON(w, 201, userResponse)

}

func decodeJSON[T any](r *http.Request) (T, error) {
	var result T
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&result)
	return result, err
}
