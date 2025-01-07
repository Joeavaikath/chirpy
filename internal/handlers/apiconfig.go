package handlers

import (
	"chirpy/internal/database"
	"sync/atomic"
)

type ApiConfig struct {
	FileserverHits atomic.Int32
	DbQueries      *database.Queries
	JwtSecret      string
	PolkaKey       string
}
