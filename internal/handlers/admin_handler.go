package handlers

import (
	"chirpy/util"
	"net/http"
	"os"
	"text/template"
)

func AdminRoutes(s *http.ServeMux, apiConfig *ApiConfig) {
	s.Handle("GET /admin/metrics", http.HandlerFunc(apiConfig.printMetric))
	s.Handle("POST /admin/reset", http.HandlerFunc(apiConfig.resetMetric))
}

type MetricPageData struct {
	Hits int32
}

func (cfg *ApiConfig) printMetric(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("./metrics/index.html"))
	data := MetricPageData{
		Hits: cfg.FileserverHits.Load(),
	}
	tmpl.Execute(w, data)
}

func (cfg *ApiConfig) resetMetric(w http.ResponseWriter, r *http.Request) {
	cfg.FileserverHits.Store(0)
	if os.Getenv("PLATFORM") != "dev" {
		invalid := util.ResponseError{}
		util.RespondWithError(w, 403, invalid)
		return
	}
	err := cfg.DbQueries.DropUsers(r.Context())
	if util.ErrorNotNil(err, w) {
		return
	}
}
