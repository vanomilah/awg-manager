package aiassistant

import (
	"context"
	"errors"
	"strings"
	"testing"

	sysexec "github.com/hoaxisr/awg-manager/internal/sys/exec"
)

type fakeResolver struct {
	addresses []string
	err       error
}

func (f fakeResolver) LookupHost(context.Context, string) ([]string, error) {
	return f.addresses, f.err
}

func TestDetectIntentExtractsDomainFromFreeFormQuestion(t *testing.T) {
	intent := DetectIntent("Проверь, почему https://Ya.Ru/search идёт напрямую")
	if intent.Kind != "domain.inspect" || intent.Entity["domain"] != "ya.ru" {
		t.Fatalf("unexpected intent: %+v", intent)
	}
}

func TestDetectIntentKeepsGreetingInChat(t *testing.T) {
	for _, question := range []string{"Привет!", "Как дела?", "Что ты умеешь?"} {
		if got := DetectIntent(question); got.Kind != "chat" {
			t.Fatalf("DetectIntent(%q) = %+v", question, got)
		}
	}
	if got := DetectIntent("Почему не работает DNS?"); got.Kind != "diagnostics.general" {
		t.Fatalf("diagnostic intent = %+v", got)
	}
}

func TestToolRegistryInspectsResolvedDomainRoutes(t *testing.T) {
	registry := &ToolRegistry{
		resolver: fakeResolver{addresses: []string{"203.0.113.2", "203.0.113.1", "203.0.113.1"}},
		run: func(_ context.Context, name string, args ...string) (*sysexec.Result, error) {
			if name != "/opt/sbin/ip" || len(args) != 3 || args[0] != "route" || args[1] != "get" {
				t.Fatalf("unsafe command shape: %s %v", name, args)
			}
			return &sysexec.Result{Stdout: args[2] + " via 192.0.2.1 dev eth0\n"}, nil
		},
	}

	intent, calls := registry.Plan("Проверь маршрут для example.com")
	if intent.Kind != "domain.inspect" || len(calls) != 1 {
		t.Fatalf("unexpected plan: %+v %+v", intent, calls)
	}
	step := registry.Execute(context.Background(), calls[0])
	if step.Status != "passed" || !step.ReadOnly || len(step.Evidence) != 3 {
		t.Fatalf("unexpected step: %+v", step)
	}
	if !strings.Contains(step.Evidence[0], "203.0.113.1, 203.0.113.2") {
		t.Fatalf("addresses are not sorted/deduplicated: %+v", step.Evidence)
	}
}

func TestToolRegistryReportsDNSFailureWithoutCommands(t *testing.T) {
	registry := &ToolRegistry{
		resolver: fakeResolver{err: errors.New("not found")},
		run: func(context.Context, string, ...string) (*sysexec.Result, error) {
			t.Fatal("command must not run after DNS failure")
			return nil, nil
		},
	}
	_, calls := registry.Plan("открой missing.example")
	step := registry.Execute(context.Background(), calls[0])
	if step.Status != "error" || !strings.Contains(step.Summary, "DNS") {
		t.Fatalf("unexpected failure step: %+v", step)
	}
}

func TestToolRegistryTreatsUnreachableRouteAsWarning(t *testing.T) {
	registry := &ToolRegistry{
		resolver: fakeResolver{addresses: []string{"2001:db8::1"}},
		run: func(context.Context, string, ...string) (*sysexec.Result, error) {
			return &sysexec.Result{Stdout: "unreachable 2001:db8::1 dev lo error -101\n"}, nil
		},
	}
	_, calls := registry.Plan("Проверь example.com")
	step := registry.Execute(context.Background(), calls[0])
	if step.Status != "warning" {
		t.Fatalf("unreachable route status = %q, want warning", step.Status)
	}
}
