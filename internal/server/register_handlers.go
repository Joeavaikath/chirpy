package server

import (
	"chirpy/internal/handlers"
	"net/http"
)

func RegisterHandlers(s *http.ServeMux, apiConfig *handlers.ApiConfig) {

	handlers := []func(*http.ServeMux, *handlers.ApiConfig){
		handlers.UserRoutes,
		handlers.AdminRoutes,
		handlers.ChirpRoutes,
		handlers.MetricsRoutes,
		handlers.TokenRoutes,
		handlers.WebhookRoutes,
	}

	for _, handler := range handlers {
		handler(s, apiConfig)
	}

}
