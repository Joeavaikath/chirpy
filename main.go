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
	polkaKey       string
}

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

	apiConfig := &apiConfig{
		dbQueries: dbQueries,
		jwtSecret: jwtSecret,
		polkaKey:  polkaKey,
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
	serveMux.Handle("POST /api/refresh", http.HandlerFunc(apiConfig.refresh))
	serveMux.Handle("POST /api/revoke", http.HandlerFunc(apiConfig.revoke))
	serveMux.Handle("POST /api/polka/webhooks", http.HandlerFunc(apiConfig.handleEvent))

	serveMux.Handle("PUT /api/users", http.HandlerFunc(apiConfig.updateUser))

	serveMux.Handle("DELETE /api/chirps/{chirpID}", http.HandlerFunc(apiConfig.deleteChirp))

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
		invalid := responseError{}
		respondWithError(w, 403, invalid)
		return
	}
	err := cfg.dbQueries.DropUsers(r.Context())
	if errorNotNil(err, w) {
		return
	}
}

type responseMessage struct {
	Message string `json:"message"`
}

type responseError struct {
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
	if errorNotNil(err, w) {
		return
	}
	hashed_password, err := auth.HashPassword(params.Password)
	if errorNotNil(err, w) {
		return
	}

	createUserParams := database.CreateUserParams{
		Email:          params.Email,
		HashedPassword: hashed_password,
	}

	user, err := cfg.dbQueries.CreateUser(r.Context(), createUserParams)
	if errorNotNil(err, w) {
		return
	}

	type User struct {
		ID          uuid.UUID `json:"id"`
		CreatedAt   time.Time `json:"created_at"`
		UpdatedAt   time.Time `json:"updated_at"`
		Email       string    `json:"email"`
		IsChirpyRed bool      `json:"is_chirpy_red"`
	}

	userCreatedResponse := User{
		ID:          user.ID,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
		Email:       params.Email,
		IsChirpyRed: user.IsChirpyRed,
	}

	respondWithJSON(w, 201, userCreatedResponse)
}

func (cfg *apiConfig) addChirp(w http.ResponseWriter, r *http.Request) {
	params, err := decodeJSON[createChirpRequest](r)
	if errorNotNil(err, w) {
		return
	}

	// Check if user has a valid JWT
	token, err := auth.GetBearerToken(r.Header)
	if errorNotNil(err, w) {
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, 401, struct {
			Error string `json:"error"`
		}{Error: err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if len(params.Body) > 140 {
		respondWithError(w, 400, responseError{
			Error: "Chirp is too long",
		})
		return
	}

	profaneList := []string{"kerfuffle", "sharbert", "fornax"}
	cleanedBody := replaceProfane(params.Body, profaneList)
	if errorNotNil(err, w) {
		return
	}

	createChirpParams := database.CreateChirpParams{
		Body:   cleanedBody,
		UserID: userID,
	}

	chirp, err := cfg.dbQueries.CreateChirp(r.Context(), createChirpParams)
	if errorNotNil(err, w) {
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

	author := r.URL.Query().Get("author_id")
	sort := r.URL.Query().Get("sort")

	var chirps []database.Chirp
	var err error

	if author == "" {
		if sort == "desc" {
			chirps, err = cfg.dbQueries.GetAllChirpsDesc(r.Context())
		} else {
			chirps, err = cfg.dbQueries.GetAllChirpsAsc(r.Context())
		}
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		author_uuid, err := uuid.Parse(author)
		if err != nil {
			respondWithError(w, http.StatusUnauthorized, err.Error())
			return
		}

		if sort == "desc" {
			chirps, err = cfg.dbQueries.GetChirpsByUserIdDesc(r.Context(), author_uuid)
		} else {
			chirps, err = cfg.dbQueries.GetChirpsByUserIdAsc(r.Context(), author_uuid)
		}

		if err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
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
	if errorNotNil(err, w) {
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
	}
	params, err := decodeJSON[loginRequest](r)
	if errorNotNil(err, w) {
		return
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
	token, err := auth.MakeJWT(searchedUser.ID, cfg.jwtSecret, time.Duration(1)*time.Hour)
	if errorNotNil(err, w) {
		return
	}

	refreshToken, err := auth.MakeRefreshToken()
	if errorNotNil(err, w) {
		return
	}

	type User struct {
		ID           uuid.UUID `json:"id"`
		CreatedAt    time.Time `json:"created_at"`
		UpdatedAt    time.Time `json:"updated_at"`
		Email        string    `json:"email"`
		Token        string    `json:"token"`
		RefreshToken string    `json:"refresh_token"`
		IsChirpyRed  bool      `json:"is_chirpy_red"`
	}

	userLoginResponse := User{
		ID:           searchedUser.ID,
		CreatedAt:    searchedUser.CreatedAt,
		UpdatedAt:    searchedUser.UpdatedAt,
		Email:        searchedUser.Email,
		Token:        token,
		RefreshToken: refreshToken,
		IsChirpyRed:  searchedUser.IsChirpyRed,
	}

	// Log into refresh token into DB
	refreshTokenParams := database.CreateRefreshTokenParams{
		Token:     refreshToken,
		UserID:    searchedUser.ID,
		ExpiresAt: time.Now().Add(60 * 24 * time.Hour),
	}
	cfg.dbQueries.CreateRefreshToken(r.Context(), refreshTokenParams)

	respondWithJSON(w, 200, userLoginResponse)

}

func (cfg *apiConfig) refresh(w http.ResponseWriter, r *http.Request) {

	authToken, err := auth.GetBearerToken(r.Header)
	if errorNotNil(err, w) {
		return
	}

	refreshToken, err := cfg.dbQueries.GetRefreshToken(r.Context(), authToken)
	if err != nil {
		respondWithError(w, 401, struct {
			Error string `json:"error"`
		}{Error: "Invalid token"})
		return
	}

	if time.Now().Compare(refreshToken.ExpiresAt) > 0 || refreshToken.RevokedAt.Valid {
		respondWithError(w, 401, struct {
			Error string `json:"error"`
		}{Error: "Expired token"})
		return
	}

	// Good to create the access token!
	accessToken, err := auth.MakeJWT(refreshToken.UserID, cfg.jwtSecret, time.Hour)
	if errorNotNil(err, w) {
		return
	}

	respondWithJSON(w, 200, struct {
		Token string `json:"token"`
	}{Token: accessToken})

}

func (cfg *apiConfig) revoke(w http.ResponseWriter, r *http.Request) {

	authToken, err := auth.GetBearerToken(r.Header)
	if errorNotNil(err, w) {
		return
	}

	err = cfg.dbQueries.RevokeRefreshToken(r.Context(), authToken)
	if err != nil {
		respondWithError(w, 404, struct {
			Error string `json:"error"`
		}{Error: "Auth token not found"})
	}

	w.WriteHeader(204)

}

func (cfg *apiConfig) updateUser(w http.ResponseWriter, r *http.Request) {
	accessToken, err := auth.GetBearerToken(r.Header)

	if accessToken == "" {
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	if errorNotNil(err, w) {
		return
	}

	userID, err := auth.ValidateJWT(accessToken, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	type updateParams struct {
		Password string `json:"password"`
		Email    string `json:"email"`
	}

	params, err := decodeJSON[updateParams](r)
	if errorNotNil(err, w) {
		return
	}

	hashedPass, err := auth.HashPassword(params.Password)
	if errorNotNil(err, w) {
		return
	}

	dbParams := database.UpdateEmailandPasswordParams{
		HashedPassword: hashedPass,
		Email:          params.Email,
		ID:             userID,
	}
	err = cfg.dbQueries.UpdateEmailandPassword(r.Context(), dbParams)
	if errorNotNil(err, w) {
		return
	}

	user, err := cfg.dbQueries.GetUserByEmail(r.Context(), dbParams.Email)
	if errorNotNil(err, w) {
		return
	}

	type User struct {
		ID          uuid.UUID `json:"id"`
		CreatedAt   time.Time `json:"created_at"`
		UpdatedAt   time.Time `json:"updated_at"`
		Email       string    `json:"email"`
		IsChirpyRed bool      `json:"is_chirpy_red"`
	}

	userResponse := User{
		ID:          user.ID,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
		Email:       user.Email,
		IsChirpyRed: user.IsChirpyRed,
	}

	respondWithJSON(w, 200, userResponse)

}

func (cfg *apiConfig) deleteChirp(w http.ResponseWriter, r *http.Request) {

	chirpID := r.PathValue("chirpID")
	chirpUUID, err := uuid.Parse(chirpID)
	if errorNotNil(err, w) {
		return
	}

	chirp, err := cfg.dbQueries.GetChirpById(r.Context(), chirpUUID)
	if errorNotNil(err, w) {
		return
	}

	accessToken, err := auth.GetBearerToken(r.Header)
	if accessToken == "" {
		respondWithError(w, 401, struct {
			Error string `json:"error"`
		}{Error: "Invalid access token"})
		return
	}

	if errorNotNil(err, w) {
		return
	}

	userID, err := auth.ValidateJWT(accessToken, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	if userID != chirp.UserID {
		respondWithError(w, http.StatusForbidden, struct {
			Error string `json:"error"`
		}{Error: "user does not own chirp"})
		return
	}

	err = cfg.dbQueries.DeleteChirpById(r.Context(), chirpUUID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, err.Error())
		return
	}

	respondWithJSON(w, http.StatusNoContent, struct {
		Error string `json:"error"`
	}{Error: "delete successful"})

}

func (cfg *apiConfig) handleEvent(w http.ResponseWriter, r *http.Request) {
	type webHookEvent struct {
		Data struct {
			UserID string `json:"user_id"`
		} `json:"data"`
		Event string `json:"event"`
	}

	apiKey, err := auth.GetAPIKey(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	if apiKey != cfg.polkaKey {
		respondWithError(w, http.StatusUnauthorized, responseMessage{
			Message: "invalid key",
		})
		return
	}

	params, err := decodeJSON[webHookEvent](r)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	if params.Event == "user.upgraded" {

		user_uuid, err := uuid.Parse(params.Data.UserID)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		err = cfg.dbQueries.GrantChirpyRed(r.Context(), user_uuid)
		if err != nil {
			respondWithError(w, http.StatusNotFound, err.Error())
			return
		}

		respondWithJSON(w, http.StatusNoContent, responseMessage{
			Message: "user upgraded",
		})
	}

	respondWithError(w, http.StatusNoContent, responseMessage{
		Message: "unknown event",
	})
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

func errorNotNil(err error, w http.ResponseWriter) bool {
	if err != nil {
		respondWithError(w, 500, error.Error(err))
		return true
	}
	return false
}
