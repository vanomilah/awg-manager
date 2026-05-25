package downloader

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	singboxorch "github.com/hoaxisr/awg-manager/internal/singbox/orchestrator"
)

type Route struct {
	Tag  string
	Kind string
}

type Outbound struct {
	Tag       string
	Kind      string
	Label     string
	Detail    string
	Available bool
}

type RouteInfo struct {
	Tag    string
	Kind   string
	Label  string
	Detail string
}

func (r RouteInfo) DisplayName() string {
	if r.Tag == "" || r.Tag == "direct" {
		return "direct"
	}
	if r.Kind != "" {
		return fmt.Sprintf("%s (%s)", r.Tag, r.Kind)
	}
	return r.Tag
}

type Lease struct {
	Client  *http.Client
	Route   RouteInfo
	cleanup func()
	once    sync.Once
}

func (l *Lease) Close() {
	if l == nil || l.cleanup == nil {
		return
	}
	l.once.Do(l.cleanup)
}

type OutboundsProvider interface {
	ListDownloadOutbounds(ctx context.Context) []Outbound
}

type SingboxOperator interface {
	IsRunning() (bool, int)
	SetSelectorDefault(ctx context.Context, selectorTag, memberTag string) error
	GetSelectorActive(ctx context.Context, selectorTag string) (string, error)
}

type SlotController interface {
	SaveSilent(slot singboxorch.Slot, jsonBytes []byte) error
	SetEnabledSilent(slot singboxorch.Slot, enabled bool) error
	Reload() error
}

type RouteProvider interface {
	GetDownloadRoute(ctx context.Context) (*Route, error)
}
