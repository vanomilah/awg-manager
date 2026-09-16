package singbox

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/hoaxisr/awg-manager/internal/logging"
)

type LogForwarder struct {
	// clashAddr — ПОСТАВЩИК адреса, а не снимок: порт Clash API меняется в
	// рантайме (issue #788), а Run переподключается каждые reconnect секунд,
	// так что новый адрес подхватывается сам, без перезапуска горутины.
	clashAddr func() string
	app       logging.AppLogger
	group     string
	engine    string

	inbound  *logging.ScopedLogger
	outbound *logging.ScopedLogger
	dns      *logging.ScopedLogger
	router   *logging.ScopedLogger
	runtime  *logging.ScopedLogger

	http *http.Client

	reconnect time.Duration
}

func NewLogForwarder(clashAddr func() string, appLogger logging.AppLogger) *LogForwarder {
	return NewEngineLogForwarder(clashAddr, appLogger, logging.GroupSingbox, "sing-box")
}

// NewEngineLogForwarder forwards a Clash-compatible /logs stream into the
// bucket of the engine that actually produced it. Both sing-box and Mihomo
// expose this endpoint, but their records must never share an identity/buffer.
func NewEngineLogForwarder(clashAddr func() string, appLogger logging.AppLogger, group, engine string) *LogForwarder {
	return &LogForwarder{
		clashAddr: clashAddr,
		app:       appLogger,
		group:     group,
		engine:    engine,
		inbound:   logging.NewScopedLogger(appLogger, group, logging.SubSBInbound),
		outbound:  logging.NewScopedLogger(appLogger, group, logging.SubSBOutbound),
		dns:       logging.NewScopedLogger(appLogger, group, logging.SubSBDNS),
		router:    logging.NewScopedLogger(appLogger, group, logging.SubSBRouter),
		runtime:   logging.NewScopedLogger(appLogger, group, logging.SubSBRuntime),
		http:      &http.Client{},
		reconnect: 3 * time.Second,
	}
}

func (f *LogForwarder) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		f.runOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(f.reconnect):
		}
	}
}

func (f *LogForwarder) runOnce(ctx context.Context) {
	level := "trace"
	if f.engine == "mihomo" {
		level = "debug"
	}
	url := fmt.Sprintf("http://%s/logs?level=%s", f.clashAddr(), level)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	resp, err := f.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		f.forward(sc.Bytes())
	}
}

type clashLogEntry struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

var timestampPrefix = regexp.MustCompile(`^[+\-]\d{4}\s+\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d+)?\s+(?:FATAL|ERROR|WARN|INFO|DEBUG|TRACE)\s+`)

var connIDPrefix = regexp.MustCompile(`^\[\d+\s+[\d.]+[a-zµ]+\]\s+`)

var contextBracket = regexp.MustCompile(`\[[^\]]*\]`)

func classifyPayload(payload string) (subgroup, target, message string) {
	return classifyPayloadForEngine(payload, "sing-box")
}

func classifyPayloadForEngine(payload, engine string) (subgroup, target, message string) {
	msg := timestampPrefix.ReplaceAllString(payload, "")
	msg = connIDPrefix.ReplaceAllString(msg, "")
	msg = strings.TrimSpace(msg)

	head, rest, hasSep := cutSegment(msg)
	if !hasSep {
		// Mihomo bracket format: [TCP], [UDP], [DNS], [Rule], [Proxy]
		if strings.HasPrefix(msg, "[") {
			if end := strings.Index(msg, "]"); end > 0 {
				bracket := strings.ToLower(msg[1:end])
				body := strings.TrimSpace(msg[end+1:])
				switch bracket {
				case "dns":
					return logging.SubSBDNS, "dns", body
				case "tcp", "udp", "inbound":
					return logging.SubSBInbound, bracket, body
				case "rule", "router", "route":
					return logging.SubSBRouter, "router", body
				case "outbound", "proxy":
					return logging.SubSBOutbound, bracket, body
				}
			}
		}
		return logging.SubSBRuntime, engine, msg
	}

	category, tag := splitCategory(head)
	switch category {
	case "inbound", "tcp", "udp":
		return logging.SubSBInbound, tagOr(tag, "inbound"), rest
	case "outbound", "proxy":
		return logging.SubSBOutbound, tagOr(tag, "outbound"), rest
	case "dns":
		return logging.SubSBDNS, tagOr(tag, "dns"), rest
	case "router", "route", "rule":
		return logging.SubSBRouter, tagOr(tag, "router"), rest
	default:
		return logging.SubSBRuntime, tagOr(tag, engine), msg
	}
}

func cutSegment(msg string) (head, rest string, ok bool) {
	idx := strings.Index(msg, ": ")
	if idx < 0 {
		return msg, "", false
	}
	return msg[:idx], strings.TrimSpace(msg[idx+2:]), true
}

func splitCategory(head string) (category, tag string) {
	if br := contextBracket.FindStringIndex(head); br != nil {
		tag = strings.TrimSpace(head[br[0]+1 : br[1]-1])
		head = strings.TrimSpace(head[:br[0]])
	}
	if i := strings.IndexAny(head, "/ "); i >= 0 {
		return head[:i], tag
	}
	return head, tag
}

func tagOr(tag, fallback string) string {
	if tag == "" {
		return fallback
	}
	return tag
}

func (f *LogForwarder) forward(line []byte) {
	if len(line) == 0 {
		return
	}
	var e clashLogEntry
	if err := json.Unmarshal(line, &e); err != nil {
		return
	}
	payload := strings.TrimSpace(e.Payload)
	if payload == "" {
		return
	}
	subgroup, target, message := classifyPayloadForEngine(payload, f.engine)
	scoped := f.scopedFor(subgroup)
	if scoped == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(e.Type)) {
	case "error", "fatal", "panic":
		scoped.Error("run", target, message)
	case "warn", "warning":
		scoped.Warn("run", target, message)
	case "info":
		scoped.Info("run", target, message)
	case "debug":
		scoped.Debug("run", target, message)
	default:
		scoped.Full("run", target, message)
	}
}

func (f *LogForwarder) scopedFor(subgroup string) *logging.ScopedLogger {
	switch subgroup {
	case logging.SubSBInbound:
		return f.inbound
	case logging.SubSBOutbound:
		return f.outbound
	case logging.SubSBDNS:
		return f.dns
	case logging.SubSBRouter:
		return f.router
	default:
		return f.runtime
	}
}
