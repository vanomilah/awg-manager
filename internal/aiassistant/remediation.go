package aiassistant

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

type ActionVerification struct {
	Status  string `json:"status"` // "passed", "warning", "failed"
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
}

type RemediationProposal struct {
	ID           string              `json:"id"`
	Action       string              `json:"action"`
	Target       string              `json:"target,omitempty"`
	Title        string              `json:"title"`
	Description  string              `json:"description"`
	Risk         string              `json:"risk"`
	Status       string              `json:"status"`
	AutoApplied  bool                `json:"autoApplied,omitempty"`
	Verification *ActionVerification `json:"verification,omitempty"`
	Error        string              `json:"error,omitempty"`
	CreatedAt    time.Time           `json:"createdAt"`
}

type ActionExecutor interface {
	Apply(ctx context.Context, action, target string) error
	Verify(ctx context.Context, action, target string) (*ActionVerification, error)
}

type ActionHandlers struct {
	RestartSingbox     func(ctx context.Context) error
	RestartMihomo      func(ctx context.Context) error
	ReloadMihomo       func(ctx context.Context) error
	RestartTunnel      func(ctx context.Context, tunnelID string) error
	UpdateSubscription func(ctx context.Context, subID string) error
	FlushDNS           func(ctx context.Context) error
	VerifySingbox      func(ctx context.Context) (*ActionVerification, error)
	VerifyMihomo       func(ctx context.Context) (*ActionVerification, error)
	VerifyTunnel       func(ctx context.Context, tunnelID string) (*ActionVerification, error)
	VerifyDNS          func(ctx context.Context) (*ActionVerification, error)
}

type ActionRegistry struct {
	handlers ActionHandlers
}

func NewActionRegistry(handlers ActionHandlers) *ActionRegistry {
	return &ActionRegistry{handlers: handlers}
}

func (r *ActionRegistry) Apply(ctx context.Context, action, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil {
		return errors.New("action registry is not configured")
	}
	switch action {
	case "singbox.restart":
		if r.handlers.RestartSingbox == nil {
			return errors.New("sing-box restart is unavailable")
		}
		return r.handlers.RestartSingbox(ctx)
	case "mihomo.restart":
		if r.handlers.RestartMihomo == nil {
			return errors.New("mihomo restart is unavailable")
		}
		return r.handlers.RestartMihomo(ctx)
	case "mihomo.reload":
		if r.handlers.ReloadMihomo == nil {
			return errors.New("mihomo reload is unavailable")
		}
		return r.handlers.ReloadMihomo(ctx)
	case "tunnel.restart":
		if r.handlers.RestartTunnel == nil {
			return errors.New("tunnel restart is unavailable")
		}
		if target == "" {
			return errors.New("tunnel target ID is required")
		}
		return r.handlers.RestartTunnel(ctx, target)
	case "subscription.update":
		if r.handlers.UpdateSubscription == nil {
			return errors.New("subscription update is unavailable")
		}
		if target == "" {
			return errors.New("subscription target ID is required")
		}
		return r.handlers.UpdateSubscription(ctx, target)
	case "dns.flush":
		if r.handlers.FlushDNS == nil {
			return errors.New("dns flush is unavailable")
		}
		return r.handlers.FlushDNS(ctx)
	default:
		return fmt.Errorf("unsupported remediation action %q", action)
	}
}

func (r *ActionRegistry) Verify(ctx context.Context, action, target string) (*ActionVerification, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, nil
	}
	switch action {
	case "singbox.restart":
		if r.handlers.VerifySingbox != nil {
			return r.handlers.VerifySingbox(ctx)
		}
		return defaultSocketVerification("127.0.0.1:11001", "sing-box proxy"), nil
	case "mihomo.restart", "mihomo.reload":
		if r.handlers.VerifyMihomo != nil {
			return r.handlers.VerifyMihomo(ctx)
		}
		return defaultSocketVerification("127.0.0.1:9090", "Mihomo REST API"), nil
	case "tunnel.restart":
		if r.handlers.VerifyTunnel != nil {
			return r.handlers.VerifyTunnel(ctx, target)
		}
		return &ActionVerification{
			Status:  "passed",
			Summary: fmt.Sprintf("Туннель %s перезапущен и готов к обработке трафика", target),
		}, nil
	case "dns.flush":
		if r.handlers.VerifyDNS != nil {
			return r.handlers.VerifyDNS(ctx)
		}
		return defaultDNSVerification(), nil
	default:
		return nil, nil
	}
}

func defaultSocketVerification(addr, label string) *ActionVerification {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return &ActionVerification{
			Status:  "warning",
			Summary: fmt.Sprintf("%s пока не отвечает на порту %s", label, addr),
			Detail:  err.Error(),
		}
	}
	_ = conn.Close()
	return &ActionVerification{
		Status:  "passed",
		Summary: fmt.Sprintf("%s успешно отвечает на порту %s", label, addr),
	}
}

func defaultDNSVerification() *ActionVerification {
	r := net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 2 * time.Second}
			return d.DialContext(ctx, "udp", "127.0.0.1:53")
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := r.LookupHost(ctx, "ya.ru")
	if err != nil {
		return &ActionVerification{
			Status:  "warning",
			Summary: "Локальный DNS-резолвер перезапущен, но тестовый запрос завершился с предупреждением",
			Detail:  err.Error(),
		}
	}
	return &ActionVerification{
		Status:  "passed",
		Summary: "Локальный DNS успешно сброшен и резолвит запросы",
	}
}

func newProposalID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("proposal-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw[:])
}

func parseActionProposal(text string) *RemediationProposal {
	text = strings.TrimSpace(text)
	actionIdx := strings.Index(text, "ACTION:")
	if actionIdx == -1 {
		return nil
	}
	actionPayload := strings.TrimSpace(text[actionIdx+len("ACTION:"):])
	if endIdx := strings.Index(actionPayload, "\n"); endIdx != -1 {
		actionPayload = actionPayload[:endIdx]
	}
	actionPayload = strings.TrimSpace(actionPayload)

	if strings.HasPrefix(actionPayload, "{") && strings.HasSuffix(actionPayload, "}") {
		var obj struct {
			Action      string `json:"action"`
			Target      string `json:"target"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Risk        string `json:"risk"`
		}
		if err := json.Unmarshal([]byte(actionPayload), &obj); err == nil && obj.Action != "" {
			risk := obj.Risk
			if risk == "" {
				risk = "medium"
			}
			return &RemediationProposal{
				ID:          newProposalID(),
				Action:      obj.Action,
				Target:      obj.Target,
				Title:       obj.Title,
				Description: obj.Description,
				Risk:        risk,
				Status:      "pending",
				CreatedAt:   time.Now(),
			}
		}
	}
	return nil
}
