package handlers

import (
	"chirpy/internal/auth"
	"chirpy/internal/database"
	"chirpy/internal/util"
	"net/http"
	"time"

	"github.com/google/uuid"
)

func UserRoutes(s *http.ServeMux, apiConfig *ApiConfig) {
	s.Handle("POST /api/users", http.HandlerFunc(apiConfig.addUser))
	s.Handle("PUT /api/users", http.HandlerFunc(apiConfig.updateUser))
}

func (cfg *ApiConfig) addUser(w http.ResponseWriter, r *http.Request) {
	type createUserRequest struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	params, err := util.DecodeJSON[createUserRequest](r)
	if util.ErrorNotNil(err, w) {
		return
	}
	hashed_password, err := auth.HashPassword(params.Password)
	if util.ErrorNotNil(err, w) {
		return
	}

	createUserParams := database.CreateUserParams{
		Email:          params.Email,
		HashedPassword: hashed_password,
	}

	user, err := cfg.DbQueries.CreateUser(r.Context(), createUserParams)
	if util.ErrorNotNil(err, w) {
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

	util.RespondWithJSON(w, 201, userCreatedResponse)
}

func (cfg *ApiConfig) updateUser(w http.ResponseWriter, r *http.Request) {
	accessToken, err := auth.GetBearerToken(r.Header)

	if accessToken == "" {
		util.RespondWithError(w, http.StatusUnauthorized, err.Error())
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

	type updateParams struct {
		Password string `json:"password"`
		Email    string `json:"email"`
	}

	params, err := util.DecodeJSON[updateParams](r)
	if util.ErrorNotNil(err, w) {
		return
	}

	hashedPass, err := auth.HashPassword(params.Password)
	if util.ErrorNotNil(err, w) {
		return
	}

	dbParams := database.UpdateEmailandPasswordParams{
		HashedPassword: hashedPass,
		Email:          params.Email,
		ID:             userID,
	}
	err = cfg.DbQueries.UpdateEmailandPassword(r.Context(), dbParams)
	if util.ErrorNotNil(err, w) {
		return
	}

	user, err := cfg.DbQueries.GetUserByEmail(r.Context(), dbParams.Email)
	if util.ErrorNotNil(err, w) {
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

	util.RespondWithJSON(w, 200, userResponse)

}
