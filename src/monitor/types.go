package monitor

import (
	"time"

	cfg "github.com/ibp-network/ibp-geodns-libs/config"
)

type Result struct {
	Member    cfg.Member
	Status    bool
	Checktime time.Time
	ErrorText string
	Data      map[string]interface{}
}
