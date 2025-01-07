package handlers

import (
	"chirpy/internal/auth"
	"chirpy/internal/database"
	"chirpy/internal/util"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

func RegisterChirpRoutes(s *http.ServeMux, apiConfig *ApiConfig) {
	s.Handle("GET /api/chirps", http.HandlerFunc(apiConfig.getAllChirps))
	s.Handle("GET /api/chirps/{chirpID}", http.HandlerFunc(apiConfig.getChirp))
	s.Handle("POST /api/chirps", http.HandlerFunc(apiConfig.addChirp))
	s.Handle("DELETE /api/chirps/{chirpID}", http.HandlerFunc(apiConfig.deleteChirp))

}

type Chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserID    uuid.UUID `json:"user_id"`
}

func (cfg *ApiConfig) addChirp(w http.ResponseWriter, r *http.Request) {
	type createChirpRequest struct {
		Body   string `json:"body"`
		UserID string `json:"user_id"`
	}
	params, err := util.DecodeJSON[createChirpRequest](r)
	if util.ErrorNotNil(err, w) {
		return
	}

	// Check if user has a valid JWT
	token, err := auth.GetBearerToken(r.Header)
	if util.ErrorNotNil(err, w) {
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.JwtSecret)
	if err != nil {
		util.RespondWithError(w, 401, struct {
			Error string `json:"error"`
		}{Error: err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if len(params.Body) > 140 {
		util.RespondWithError(w, 400, util.ResponseError{
			Error: "Chirp is too long",
		})
		return
	}

	profaneList := []string{"kerfuffle", "sharbert", "fornax"}
	cleanedBody := replaceProfane(params.Body, profaneList)
	if util.ErrorNotNil(err, w) {
		return
	}

	createChirpParams := database.CreateChirpParams{
		Body:   cleanedBody,
		UserID: userID,
	}

	chirp, err := cfg.DbQueries.CreateChirp(r.Context(), createChirpParams)
	if util.ErrorNotNil(err, w) {
		return
	}

	chirpCreatedResponse := Chirp{
		ID:        chirp.ID,
		CreatedAt: chirp.CreatedAt,
		UpdatedAt: chirp.UpdatedAt,
		Body:      chirp.Body,
		UserID:    chirp.UserID,
	}

	util.RespondWithJSON(w, 201, chirpCreatedResponse)

}

func (cfg *ApiConfig) getAllChirps(w http.ResponseWriter, r *http.Request) {

	author := r.URL.Query().Get("author_id")
	sort := r.URL.Query().Get("sort")

	var chirps []database.Chirp
	var err error

	if author == "" {
		if sort == "desc" {
			chirps, err = cfg.DbQueries.GetAllChirpsDesc(r.Context())
		} else {
			chirps, err = cfg.DbQueries.GetAllChirpsAsc(r.Context())
		}
		if err != nil {
			util.RespondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		author_uuid, err := uuid.Parse(author)
		if err != nil {
			util.RespondWithError(w, http.StatusUnauthorized, err.Error())
			return
		}

		if sort == "desc" {
			chirps, err = cfg.DbQueries.GetChirpsByUserIdDesc(r.Context(), author_uuid)
		} else {
			chirps, err = cfg.DbQueries.GetChirpsByUserIdAsc(r.Context(), author_uuid)
		}

		if err != nil {
			util.RespondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if err != nil {
		util.RespondWithError(w, http.StatusUnauthorized, err.Error())
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
	util.RespondWithJSON(w, 200, responseChirps)
}

func (cfg *ApiConfig) getChirp(w http.ResponseWriter, r *http.Request) {
	chirpID := r.PathValue("chirpID")
	chirpUUID, err := uuid.Parse(chirpID)
	if util.ErrorNotNil(err, w) {
		return
	}
	chirp, err := cfg.DbQueries.GetChirpById(r.Context(), chirpUUID)
	if err != nil {
		if err == sql.ErrNoRows {
			util.RespondWithError(w, http.StatusNotFound, struct {
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
	util.RespondWithJSON(w, 200, responseChirp)

}

func replaceProfane(message string, profaneList []string) string {
	words := strings.Split(message, " ")
	cleanedWords := []string{}
	for _, word := range words {
		if util.SliceContains(profaneList, strings.ToLower(word)) {
			cleanedWords = append(cleanedWords, "****")
		} else {
			cleanedWords = append(cleanedWords, word)
		}
	}
	return strings.Join(cleanedWords, " ")
}

func (cfg *ApiConfig) deleteChirp(w http.ResponseWriter, r *http.Request) {

	chirpID := r.PathValue("chirpID")
	chirpUUID, err := uuid.Parse(chirpID)
	if util.ErrorNotNil(err, w) {
		return
	}

	chirp, err := cfg.DbQueries.GetChirpById(r.Context(), chirpUUID)
	if util.ErrorNotNil(err, w) {
		return
	}

	accessToken, err := auth.GetBearerToken(r.Header)
	if accessToken == "" {
		util.RespondWithError(w, 401, struct {
			Error string `json:"error"`
		}{Error: "Invalid access token"})
		return
	}

	if util.ErrorNotNil(err, w) {
		return
	}

	userID, err := auth.ValidateJWT(accessToken, cfg.JwtSecret)
	if err != nil {
		util.RespondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	if userID != chirp.UserID {
		util.RespondWithError(w, http.StatusForbidden, struct {
			Error string `json:"error"`
		}{Error: "user does not own chirp"})
		return
	}

	err = cfg.DbQueries.DeleteChirpById(r.Context(), chirpUUID)
	if err != nil {
		util.RespondWithError(w, http.StatusNotFound, err.Error())
		return
	}

	util.RespondWithJSON(w, http.StatusNoContent, struct {
		Error string `json:"error"`
	}{Error: "delete successful"})

}
