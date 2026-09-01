package aiassistant

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	sysexec "github.com/hoaxisr/awg-manager/internal/sys/exec"
)

type ToolCall struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

type ToolStep struct {
	Name       string        `json:"name"`
	Title      string        `json:"title"`
	Status     string        `json:"status"`
	Summary    string        `json:"summary"`
	Evidence   []string      `json:"evidence,omitempty"`
	DurationMS int64         `json:"durationMs"`
	ReadOnly   bool          `json:"readOnly"`
	StartedAt  time.Time     `json:"startedAt"`
	Duration   time.Duration `json:"-"`
}

type ToolExecutor interface {
	Plan(string) (Intent, []ToolCall)
	Execute(context.Context, ToolCall) ToolStep
}

type hostResolver interface {
	LookupHost(context.Context, string) ([]string, error)
}

type commandRunner func(context.Context, string, ...string) (*sysexec.Result, error)

type ToolRegistry struct {
	resolver hostResolver
	run      commandRunner
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{resolver: net.DefaultResolver, run: sysexec.Run}
}

func (r *ToolRegistry) Plan(question string) (Intent, []ToolCall) {
	intent := DetectIntent(question)
	if intent.Kind == "domain.inspect" {
		return intent, []ToolCall{{Name: "domain.inspect", Arguments: intent.Entity}}
	}
	return intent, nil
}

func (r *ToolRegistry) Execute(parent context.Context, call ToolCall) ToolStep {
	started := time.Now()
	step := ToolStep{Name: call.Name, Status: "error", ReadOnly: true, StartedAt: started, Evidence: []string{}}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()

	switch call.Name {
	case "domain.inspect":
		step.Title = "Проверка домена и маршрута"
		step.Summary, step.Evidence, step.Status = r.inspectDomain(ctx, call.Arguments["domain"])
	default:
		step.Title = "Неизвестный инструмент"
		step.Summary = "Инструмент не зарегистрирован"
	}
	step.Duration = time.Since(started)
	step.DurationMS = step.Duration.Milliseconds()
	return step
}

func (r *ToolRegistry) inspectDomain(ctx context.Context, domain string) (string, []string, string) {
	if !validDomain(domain) {
		return "Некорректное имя домена", nil, "error"
	}
	addresses, err := r.resolver.LookupHost(ctx, domain)
	if err != nil {
		return "DNS не разрешил домен", []string{fmt.Sprintf("%s: %v", domain, err)}, "error"
	}
	sort.Strings(addresses)
	addresses = uniqueStrings(addresses)
	if len(addresses) > 4 {
		addresses = addresses[:4]
	}
	evidence := []string{fmt.Sprintf("%s -> %s", domain, strings.Join(addresses, ", "))}
	status := "passed"
	for _, address := range addresses {
		result, routeErr := r.run(ctx, "/opt/sbin/ip", "route", "get", address)
		if routeErr != nil {
			status = "warning"
			evidence = append(evidence, fmt.Sprintf("route %s: %v", address, routeErr))
			continue
		}
		route := strings.TrimSpace(result.Stdout)
		if route == "" {
			status = "warning"
			route = "маршрут не найден"
		}
		route = firstLine(route)
		if strings.HasPrefix(strings.ToLower(route), "unreachable") {
			status = "warning"
		}
		evidence = append(evidence, fmt.Sprintf("route %s: %s", address, route))
	}
	return fmt.Sprintf("Домен %s разрешён; проверено маршрутов: %d", domain, len(addresses)), evidence, status
}

func validDomain(value string) bool {
	match := domainPattern.FindStringSubmatch(value)
	return len(match) > 1 && strings.EqualFold(match[1], value) && len(value) <= 253
}

func uniqueStrings(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func firstLine(value string) string {
	if before, _, ok := strings.Cut(value, "\n"); ok {
		return before
	}
	return value
}
