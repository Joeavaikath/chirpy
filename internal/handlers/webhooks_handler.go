package handlers

import (
	"chirpy/internal/auth"
	"chirpy/internal/util"
	"net/http"

	"github.com/google/uuid"
)

func WebhookRoutes(s *http.ServeMux, apiConfig *ApiConfig) {
	s.Handle("POST /api/polka/webhooks", http.HandlerFunc(apiConfig.handleEvent))

}

func (cfg *ApiConfig) handleEvent(w http.ResponseWriter, r *http.Request) {
	type webHookEvent struct {
		Data struct {
			UserID string `json:"user_id"`
		} `json:"data"`
		Event string `json:"event"`
	}

	apiKey, err := auth.GetAPIKey(r.Header)
	if err != nil {
		util.RespondWithError(w, http.StatusUnauthorized, err.Error())
		return
	}

	if apiKey != cfg.PolkaKey {
		util.RespondWithError(w, http.StatusUnauthorized, util.ResponseMessage{
			Message: "invalid key",
		})
		return
	}

	params, err := util.DecodeJSON[webHookEvent](r)
	if err != nil {
		util.RespondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	if params.Event == "user.upgraded" {

		user_uuid, err := uuid.Parse(params.Data.UserID)
		if err != nil {
			util.RespondWithError(w, http.StatusInternalServerError, err.Error())
			return
		}

		err = cfg.DbQueries.GrantChirpyRed(r.Context(), user_uuid)
		if err != nil {
			util.RespondWithError(w, http.StatusNotFound, err.Error())
			return
		}

		util.RespondWithJSON(w, http.StatusNoContent, util.ResponseMessage{
			Message: "user upgraded",
		})
	}

	util.RespondWithError(w, http.StatusNoContent, util.ResponseMessage{
		Message: "unknown event",
	})
}
