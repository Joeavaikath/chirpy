package handlers

import (
	"chirpy/internal/auth"
	"chirpy/internal/database"
	"chirpy/util"
	"net/http"
	"time"

	"github.com/google/uuid"
)

func TokenRoutes(s *http.ServeMux, apiConfig *ApiConfig) {
	s.Handle("POST /api/login", http.HandlerFunc(apiConfig.login))
	s.Handle("POST /api/refresh", http.HandlerFunc(apiConfig.refresh))
	s.Handle("POST /api/revoke", http.HandlerFunc(apiConfig.revoke))
}

func (cfg *ApiConfig) login(w http.ResponseWriter, r *http.Request) {
	type loginRequest struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	params, err := util.DecodeJSON[loginRequest](r)
	if util.ErrorNotNil(err, w) {
		return
	}

	searchedUser, err := cfg.DbQueries.GetUserByEmail(r.Context(), params.Email)
	if err != nil {
		util.RespondWithError(w, 401, struct {
			Error string `json:"error"`
		}{Error: "Incorrect email or password"})
		return
	}

	if auth.CheckPasswordHash(searchedUser.HashedPassword, params.Password) != nil {

		util.RespondWithError(w, 401, struct {
			Error string `json:"error"`
		}{Error: "Incorrect email or password"})
		return
	}

	// All okay, generate the token
	token, err := auth.MakeJWT(searchedUser.ID, cfg.JwtSecret, time.Duration(1)*time.Hour)
	if util.ErrorNotNil(err, w) {
		return
	}

	refreshToken, err := auth.MakeRefreshToken()
	if util.ErrorNotNil(err, w) {
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
	cfg.DbQueries.CreateRefreshToken(r.Context(), refreshTokenParams)

	util.RespondWithJSON(w, 200, userLoginResponse)

}

func (cfg *ApiConfig) refresh(w http.ResponseWriter, r *http.Request) {

	authToken, err := auth.GetBearerToken(r.Header)
	if util.ErrorNotNil(err, w) {
		return
	}

	refreshToken, err := cfg.DbQueries.GetRefreshToken(r.Context(), authToken)
	if err != nil {
		util.RespondWithError(w, 401, struct {
			Error string `json:"error"`
		}{Error: "Invalid token"})
		return
	}

	if time.Now().Compare(refreshToken.ExpiresAt) > 0 || refreshToken.RevokedAt.Valid {
		util.RespondWithError(w, 401, struct {
			Error string `json:"error"`
		}{Error: "Expired token"})
		return
	}

	// Good to create the access token!
	accessToken, err := auth.MakeJWT(refreshToken.UserID, cfg.JwtSecret, time.Hour)
	if util.ErrorNotNil(err, w) {
		return
	}

	util.RespondWithJSON(w, 200, struct {
		Token string `json:"token"`
	}{Token: accessToken})

}

func (cfg *ApiConfig) revoke(w http.ResponseWriter, r *http.Request) {

	authToken, err := auth.GetBearerToken(r.Header)
	if util.ErrorNotNil(err, w) {
		return
	}

	err = cfg.DbQueries.RevokeRefreshToken(r.Context(), authToken)
	if err != nil {
		util.RespondWithError(w, 404, struct {
			Error string `json:"error"`
		}{Error: "Auth token not found"})
	}

	w.WriteHeader(204)

}
