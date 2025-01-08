package handlers

import "net/http"

func RegisterHandlers(s *http.ServeMux, apiConfig *ApiConfig) {

	handlers := []func(*http.ServeMux, *ApiConfig){
		UserRoutes,
		AdminRoutes,
		ChirpRoutes,
		MetricsRoutes,
		TokenRoutes,
		WebhookRoutes,
	}

	for _, handler := range handlers {
		handler(s, apiConfig)
	}

}
