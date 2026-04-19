package status

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sysinner/incore/v2/internal/config"
)

var (
	ZoneletLeader        string
	ZoneletLeaderTTL     int64 = 12000 // milliseconds
	ZoneletLeaderUpdated int64
	ZoneletLeaderVersion uint64
)

var (
	Zonelet_HostStatusSet sync.Map

	Zonelet_ForceRefresh atomic.Bool
)

func IsZonelet() bool {
	if config.Config.Zonelet.ZoneName == "" {
		return false
	}
	for _, v := range config.Config.Server.ZoneHosts {
		if strings.HasPrefix(v, config.Config.Hostlet.LanAddr+":") {
			return true
		}
	}
	return false
}

func IsZoneletLeader() bool {
	tn := time.Now().UnixMilli()
	return ZoneletLeader == config.Config.Hostlet.HostId &&
		(ZoneletLeaderUpdated+ZoneletLeaderTTL) > tn
}
