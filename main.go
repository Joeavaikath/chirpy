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

	serveMux.Handle("POST /api/users", http.HandlerFunc(apiConfig.createUser))
	serveMux.Handle("GET /api/chirps", http.HandlerFunc(apiConfig.getAllChirps))
	serveMux.Handle("GET /api/chirps/{chirpID}", http.HandlerFunc(apiConfig.getChirp))
	serveMux.Handle("POST /api/chirps", http.HandlerFunc(apiConfig.addChirp))

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
		return
	}
	err := cfg.dbQueries.DropUsers(r.Context())
	if somethingWentWrongCheck(err, w) {
		return
	}
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

type createChirpRequest struct {
	Body   string `json:"body"`
	UserID string `json:"user_id"`
}

type User struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
}

type Chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserID    uuid.UUID `json:"user_id"`
}

func validateChirp(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	decoder := json.NewDecoder(r.Body)
	params := chirp{}
	err := decoder.Decode(&params)

	if somethingWentWrongCheck(err, w) {
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
	return
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
	if somethingWentWrongCheck(err, w) {
		return
	}

	user, err := cfg.dbQueries.CreateUser(r.Context(), params.Email)
	if somethingWentWrongCheck(err, w) {
		return
	}

	userCreatedResponse := User{
		ID:        user.ID,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
		Email:     params.Email,
	}

	respondWithJSON(w, 201, userCreatedResponse)
}

func (cfg *apiConfig) addChirp(w http.ResponseWriter, r *http.Request) {
	params, err := decodeJSON[createChirpRequest](r)
	if somethingWentWrongCheck(err, w) {
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if len(params.Body) > 140 {
		respondWithError(w, 400, invalidChirp{
			Error: "Chirp is too long",
		})
		return
	}

	profaneList := []string{"kerfuffle", "sharbert", "fornax"}
	cleanedBody := replaceProfane(params.Body, profaneList)
	user_id, err := uuid.Parse(params.UserID)
	if somethingWentWrongCheck(err, w) {
		return
	}

	createChirpParams := database.CreateChirpParams{
		Body:   cleanedBody,
		UserID: user_id,
	}

	chirp, err := cfg.dbQueries.CreateChirp(r.Context(), createChirpParams)
	if somethingWentWrongCheck(err, w) {
		return
	}

	chirpCreatedResponse := Chirp{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserID:    chirp.UserID,
	}

	respondWithJSON(w, 201, chirpCreatedResponse)

}

func (cfg *apiConfig) getAllChirps(w http.ResponseWriter, r *http.Request) {
	chirps, err := cfg.dbQueries.GetAllChirps(r.Context())
	if somethingWentWrongCheck(err, w) {
		return
	}
	responseChirps := []Chirp{}
	for _, chirp := range chirps {
		chirpResponse := Chirp{
			ID:        chirp.ID,
			CreatedAt: chirp.CreatedAt,
			UpdatedAt: chirp.UpdatedAt,
			Body:      chirp.Body,
			UserID:    chirp.UserID,
		}
		responseChirps = append(responseChirps, chirpResponse)
	}
	respondWithJSON(w, 200, responseChirps)
}

func (cfg *apiConfig) getChirp(w http.ResponseWriter, r *http.Request) {
	chirpID := r.PathValue("chirpID")
	chirpUUID, err := uuid.Parse(chirpID)
	if somethingWentWrongCheck(err, w) {
		return
	}
	chirp, err := cfg.dbQueries.GetChirpById(r.Context(), chirpUUID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondWithError(w, http.StatusNotFound, struct {
				Error string `json:"error"`
			}{Error: "Chirp not found"})
			return
		}
	}
	responseChirp := Chirp{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserID:    chirp.UserID,
	}
	respondWithJSON(w, 200, responseChirp)

}

func decodeJSON[T any](r *http.Request) (T, error) {
	var result T
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&result)
	return result, err
}

func somethingWentWrongCheck(err error, w http.ResponseWriter) bool {
	if err != nil {
		respondWithError(w, 500, invalidChirp{
			Error: "Something went wrong",
		})
		return true
	}
	return false
}
