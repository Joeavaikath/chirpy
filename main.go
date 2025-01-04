package main

import (
	"chirpy/internal/auth"
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
	jwtSecret      string
}

func main() {

	godotenv.Load()

	dbURL := os.Getenv("DB_URL")
	jwtSecret := os.Getenv("SECRET")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		os.Exit(1)
	}

	dbQueries := database.New(db)

	serveMux := http.NewServeMux()

	apiConfig := &apiConfig{
		dbQueries: dbQueries,
		jwtSecret: jwtSecret,
	}

	serveMux.Handle("/app/", apiConfig.middlewareMetricsInc(http.StripPrefix("/app", http.FileServer(http.Dir(".")))))
	serveMux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	serveMux.Handle("GET /admin/metrics", http.HandlerFunc(apiConfig.printMetric))
	serveMux.Handle("POST /admin/reset", http.HandlerFunc(apiConfig.resetMetric))

	serveMux.Handle("POST /api/users", http.HandlerFunc(apiConfig.addUser))
	serveMux.Handle("GET /api/chirps", http.HandlerFunc(apiConfig.getAllChirps))
	serveMux.Handle("GET /api/chirps/{chirpID}", http.HandlerFunc(apiConfig.getChirp))
	serveMux.Handle("POST /api/chirps", http.HandlerFunc(apiConfig.addChirp))
	serveMux.Handle("POST /api/login", http.HandlerFunc(apiConfig.login))

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

type invalidChirp struct {
	Error string `json:"error"`
}

type createUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type createChirpRequest struct {
	Body   string `json:"body"`
	UserID string `json:"user_id"`
}

type Chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserID    uuid.UUID `json:"user_id"`
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

func (cfg *apiConfig) addUser(w http.ResponseWriter, r *http.Request) {
	params, err := decodeJSON[createUserRequest](r)
	if somethingWentWrongCheck(err, w) {
		return
	}
	hashed_password, err := auth.HashPassword(params.Password)
	if somethingWentWrongCheck(err, w) {
		return
	}

	createUserParams := database.CreateUserParams{
		Email:          params.Email,
		HashedPassword: hashed_password,
	}

	user, err := cfg.dbQueries.CreateUser(r.Context(), createUserParams)
	if somethingWentWrongCheck(err, w) {
		return
	}

	type User struct {
		ID        uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Email     string    `json:"email"`
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

	// Check if user has a valid JWT
	token, err := auth.GetBearerToken(r.Header)
	if somethingWentWrongCheck(err, w) {
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if somethingWentWrongCheck(err, w) {
		return
	}

	if userID == uuid.Nil {
		respondWithError(w, 401, struct {
			Error string `json:"error"`
		}{Error: "Unauthorized"})
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
	if somethingWentWrongCheck(err, w) {
		return
	}

	createChirpParams := database.CreateChirpParams{
		Body:   cleanedBody,
		UserID: userID,
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

func (cfg *apiConfig) login(w http.ResponseWriter, r *http.Request) {
	type loginRequest struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Expiry   int    `json:"expiry_in_seconds"`
	}
	params, err := decodeJSON[loginRequest](r)
	if somethingWentWrongCheck(err, w) {
		return
	}

	// If 0 value or above threshold
	if params.Expiry == 0 || params.Expiry > 3600 {
		params.Expiry = 3600
	}

	searchedUser, err := cfg.dbQueries.GetUserByEmail(r.Context(), params.Email)
	if err != nil {
		respondWithError(w, 401, struct {
			Error string `json:"error"`
		}{Error: "Incorrect email or password"})
		return
	}

	if auth.CheckPasswordHash(searchedUser.HashedPassword, params.Password) != nil {

		respondWithError(w, 401, struct {
			Error string `json:"error"`
		}{Error: "Incorrect email or password"})
		return
	}

	// All okay, generate the token
	token, err := auth.MakeJWT(searchedUser.ID, cfg.jwtSecret, time.Duration(params.Expiry)*time.Second)
	if somethingWentWrongCheck(err, w) {
		return
	}

	type User struct {
		ID        uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
		Email     string    `json:"email"`
		Token     string    `json:"token"`
	}

	userLoginResponse := User{
		ID:        searchedUser.ID,
		CreatedAt: searchedUser.CreatedAt,
		UpdatedAt: searchedUser.UpdatedAt,
		Email:     searchedUser.Email,
		Token:     token,
	}
	respondWithJSON(w, 200, userLoginResponse)

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

func decodeJSON[T any](r *http.Request) (T, error) {
	var result T
	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&result)
	return result, err
}

func somethingWentWrongCheck(err error, w http.ResponseWriter) bool {
	if err != nil {
		respondWithError(w, 500, error.Error(err))
		return true
	}
	return false
}
