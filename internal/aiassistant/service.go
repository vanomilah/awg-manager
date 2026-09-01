// Package aiassistant provides the safe, local diagnostic core used by the
// system AI assistant. It deliberately has no shell or configuration write
// access: all router data comes from the existing sanitized diagnostics runner.
package aiassistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hoaxisr/awg-manager/internal/diagnostics"
)

var ErrRunning = errors.New("AI diagnostics are already running")

type DiagnosticsRunner interface {
	RunWithStream(context.Context, diagnostics.RunOptions) (<-chan diagnostics.DiagEvent, error)
	Result() ([]byte, error)
}

type Finding struct {
	Severity       string `json:"severity"`
	Title          string `json:"title"`
	Detail         string `json:"detail"`
	Recommendation string `json:"recommendation"`
	Source         string `json:"source"`
}

type Stats struct {
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

type ChatMessage struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

type State struct {
	Status      string               `json:"status"`
	Progress    string               `json:"progress,omitempty"`
	Question    string               `json:"question,omitempty"`
	Summary     string               `json:"summary,omitempty"`
	Engine      string               `json:"engine"`
	ReadOnly    bool                 `json:"readOnly"`
	StartedAt   time.Time            `json:"startedAt,omitempty"`
	CompletedAt time.Time            `json:"completedAt,omitempty"`
	Stats       Stats                `json:"stats"`
	Findings    []Finding            `json:"findings"`
	ModelAnswer string               `json:"modelAnswer,omitempty"`
	ModelError  string               `json:"modelError,omitempty"`
	Intent      Intent               `json:"intent"`
	ToolSteps   []ToolStep           `json:"toolSteps"`
	Error       string               `json:"error,omitempty"`
	Messages    []ChatMessage        `json:"messages"`
	Proposal    *RemediationProposal `json:"proposal,omitempty"`
}

type Service struct {
	runner   DiagnosticsRunner
	config   *ConfigStore
	model    ModelAnalyzer
	embedded *EmbeddedManager
	tools    ToolExecutor
	actions  ActionExecutor

	mu    sync.RWMutex
	state State
}

func (s *Service) SetModel(config *ConfigStore, model ModelAnalyzer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = config
	s.model = model
}

func (s *Service) SetEmbedded(embedded *EmbeddedManager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.embedded = embedded
}

func (s *Service) SetTools(tools ToolExecutor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = tools
}

func (s *Service) SetActions(actions ActionExecutor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actions = actions
}

func NewService(runner DiagnosticsRunner) *Service {
	return &Service{
		runner: runner,
		state: State{
			Status:    "idle",
			Engine:    "local-diagnostics",
			ReadOnly:  true,
			Findings:  []Finding{},
			ToolSteps: []ToolStep{},
			Messages:  []ChatMessage{},
		},
	}
}

func (s *Service) Start(question string) error {
	question = strings.TrimSpace(question)
	if len([]rune(question)) > 1000 {
		return fmt.Errorf("question is too long")
	}

	s.mu.Lock()
	if s.state.Status == "running" {
		s.mu.Unlock()
		return ErrRunning
	}
	messages := append([]ChatMessage(nil), s.state.Messages...)
	messages = appendChatMessage(messages, ChatMessage{Role: "user", Content: question, CreatedAt: time.Now()})
	s.state = State{
		Status:    "running",
		Progress:  "Подготовка безопасной диагностики…",
		Question:  question,
		Engine:    "local-diagnostics",
		ReadOnly:  true,
		StartedAt: time.Now(),
		Findings:  []Finding{},
		ToolSteps: []ToolStep{},
		Messages:  messages,
	}
	s.mu.Unlock()

	go s.run()
	return nil
}

func (s *Service) Status() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state := s.state
	state.Findings = append([]Finding(nil), s.state.Findings...)
	state.ToolSteps = append([]ToolStep(nil), s.state.ToolSteps...)
	state.Messages = append([]ChatMessage(nil), s.state.Messages...)
	return state
}

func (s *Service) run() {
	s.mu.RLock()
	question, tools := s.state.Question, s.tools
	s.mu.RUnlock()
	targeted := false
	intent := Intent{Kind: "diagnostics.general"}
	if tools != nil {
		var calls []ToolCall
		intent, calls = tools.Plan(question)
		targeted = len(calls) > 0
		s.mu.Lock()
		s.state.Intent = intent
		s.mu.Unlock()
		for _, call := range calls {
			s.mu.Lock()
			s.state.Progress = "Выполняется адресная проверка: " + call.Name
			s.mu.Unlock()
			step := tools.Execute(context.Background(), call)
			s.mu.Lock()
			s.state.ToolSteps = append(s.state.ToolSteps, step)
			s.mu.Unlock()
		}
	}
	if intent.Kind == "chat" {
		s.completeChat()
		return
	}
	if targeted {
		s.completeTargeted()
		return
	}

	if s.runner == nil {
		s.fail(errors.New("diagnostics runner is not configured"))
		return
	}

	ch, err := s.runner.RunWithStream(context.Background(), diagnostics.RunOptions{IncludeRestart: false})
	if err != nil {
		s.fail(err)
		return
	}

	for event := range ch {
		s.mu.Lock()
		switch event.Type {
		case "phase":
			if event.Label != "" {
				s.state.Progress = event.Label
			}
		case "error":
			if event.Message != "" {
				s.state.Error = event.Message
			}
		}
		s.mu.Unlock()
	}

	raw, err := s.runner.Result()
	if err != nil {
		s.fail(err)
		return
	}
	var report diagnostics.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		s.fail(fmt.Errorf("decode diagnostics report: %w", err))
		return
	}

	stats, findings := analyze(report)
	s.mu.RLock()
	toolSteps := append([]ToolStep(nil), s.state.ToolSteps...)
	s.mu.RUnlock()
	modelAnswer, modelErr, engineName := s.runModel(report, findings, toolSteps)
	s.completeDiagnosis(stats, findings, modelAnswer, modelErr, engineName)
}

func (s *Service) completeDiagnosis(stats Stats, findings []Finding, modelAnswer string, modelErr error, engineName string) {
	s.mu.Lock()
	s.state.Status = "done"
	s.state.Progress = "Диагностика завершена"
	s.state.CompletedAt = time.Now()
	s.state.Stats = stats
	s.state.Findings = findings
	s.state.ModelAnswer = modelAnswer
	if modelErr != nil {
		s.state.ModelError = modelErr.Error()
	}
	if modelAnswer != "" && engineName != "" {
		s.state.Engine = engineName
	}
	s.state.Summary = buildSummary(stats, len(findings))
	proposal := remediationForFindings(findings, modelAnswer)
	s.state.Proposal = proposal
	answer := modelAnswer
	if answer == "" && modelErr != nil {
		answer = "Модельный анализ недоступен: " + modelErr.Error()
	}
	if answer == "" {
		answer = s.state.Summary
	}
	s.state.Messages = appendChatMessage(s.state.Messages, ChatMessage{Role: "assistant", Content: answer, CreatedAt: time.Now()})
	s.state.Error = ""

	autoFixEnabled := false
	if s.config != nil {
		autoFixEnabled = s.config.Get().AutoFix
	}
	shouldAutoFix := autoFixEnabled && proposal != nil && (proposal.Risk == "low" || proposal.Risk == "medium")
	if shouldAutoFix {
		proposal.AutoApplied = true
	}
	s.mu.Unlock()

	if shouldAutoFix {
		_ = s.ApplyAction(proposal.ID)
	}
}

func remediationForFindings(findings []Finding, modelAnswer string) *RemediationProposal {
	if proposal := parseActionProposal(modelAnswer); proposal != nil {
		return proposal
	}
	for _, finding := range findings {
		switch finding.Source {
		case "singbox_proxy_port", "singbox_runtime":
			return &RemediationProposal{
				ID:          newProposalID(),
				Action:      "singbox.restart",
				Title:       "Перезапустить sing-box",
				Description: "Перезапустить процесс sing-box и заново поднять его локальные proxy-порты. Текущие соединения через sing-box кратковременно прервутся.",
				Risk:        "medium",
				Status:      "pending",
				CreatedAt:   time.Now(),
			}
		case "mihomo_runtime", "mihomo_api":
			return &RemediationProposal{
				ID:          newProposalID(),
				Action:      "mihomo.restart",
				Title:       "Перезапустить Mihomo",
				Description: "Перезапустить процесс Mihomo и переинициализировать сетевые правила.",
				Risk:        "medium",
				Status:      "pending",
				CreatedAt:   time.Now(),
			}
		case "tunnel_handshake_stale", "tunnel_down":
			target := finding.Detail
			return &RemediationProposal{
				ID:          newProposalID(),
				Action:      "tunnel.restart",
				Target:      target,
				Title:       "Перезапустить туннель " + target,
				Description: "Выполнить повторную инициализацию интерфейса туннеля и восстановить handshake.",
				Risk:        "low",
				Status:      "pending",
				CreatedAt:   time.Now(),
			}
		case "dns_lookup_failed", "dns_leak":
			return &RemediationProposal{
				ID:          newProposalID(),
				Action:      "dns.flush",
				Title:       "Сбросить DNS-кэш",
				Description: "Перезапустить локальный резолвер и сбросить кэш DNS.",
				Risk:        "low",
				Status:      "pending",
				CreatedAt:   time.Now(),
			}
		case "subscription_error":
			target := finding.Detail
			return &RemediationProposal{
				ID:          newProposalID(),
				Action:      "subscription.update",
				Target:      target,
				Title:       "Обновить подписку " + target,
				Description: "Принудительно загрузить обновлённый список серверов подписки.",
				Risk:        "low",
				Status:      "pending",
				CreatedAt:   time.Now(),
			}
		}
	}
	return nil
}

func (s *Service) ApplyAction(id string) error {
	s.mu.Lock()
	proposal := s.state.Proposal
	actions := s.actions
	if proposal == nil || proposal.ID != id || (proposal.Status != "pending" && !proposal.AutoApplied) {
		s.mu.Unlock()
		return errors.New("remediation proposal is unavailable or expired")
	}
	if time.Since(proposal.CreatedAt) > 10*time.Minute {
		proposal.Status = "expired"
		s.mu.Unlock()
		return errors.New("remediation proposal expired")
	}
	proposal.Status = "applying"
	s.mu.Unlock()
	if actions == nil {
		return s.finishAction(errors.New("remediation executor is unavailable"), nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := actions.Apply(ctx, proposal.Action, proposal.Target)
	var ver *ActionVerification
	if err == nil {
		time.Sleep(1 * time.Second)
		ver, _ = actions.Verify(ctx, proposal.Action, proposal.Target)
	}
	return s.finishAction(err, ver)
}

func (s *Service) finishAction(err error, ver *ActionVerification) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Proposal == nil {
		return err
	}
	if err != nil {
		s.state.Proposal.Status = "failed"
		s.state.Proposal.Error = err.Error()
		return err
	}
	s.state.Proposal.Status = "applied"
	s.state.Proposal.Verification = ver
	msg := "Исправление применено: " + s.state.Proposal.Title + "."
	if s.state.Proposal.AutoApplied {
		msg = "ИИ автоматически применил исправление: " + s.state.Proposal.Title + "."
	}
	if ver != nil && ver.Summary != "" {
		msg += "\nПроверка: " + ver.Summary
	}
	s.state.Messages = appendChatMessage(s.state.Messages, ChatMessage{Role: "assistant", Content: msg, CreatedAt: time.Now()})
	return nil
}

func (s *Service) completeChat() {
	answer, modelErr, engineName := s.runModel(diagnostics.Report{}, nil, nil)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Status = "done"
	s.state.Progress = "Ответ готов"
	s.state.CompletedAt = time.Now()
	s.state.ModelAnswer = answer
	if modelErr != nil {
		s.state.ModelError = modelErr.Error()
	}
	if answer != "" && engineName != "" {
		s.state.Engine = engineName
	}
	if answer == "" {
		if modelErr != nil {
			answer = "Модельный анализ недоступен: " + modelErr.Error()
		} else {
			answer = "Модель для обычного диалога не подключена."
		}
	}
	s.state.Messages = appendChatMessage(s.state.Messages, ChatMessage{Role: "assistant", Content: answer, CreatedAt: time.Now()})
}

func (s *Service) completeTargeted() {
	s.mu.RLock()
	steps := append([]ToolStep(nil), s.state.ToolSteps...)
	s.mu.RUnlock()
	stats := Stats{}
	for _, step := range steps {
		switch step.Status {
		case "passed":
			stats.Passed++
		case "warning", "error":
			stats.Failed++
		default:
			stats.Skipped++
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Status = "done"
	s.state.Progress = "Адресная проверка завершена"
	s.state.CompletedAt = time.Now()
	s.state.Stats = stats
	s.state.Summary = fmt.Sprintf("Выполнено адресных проверок: %d. Изменения не применялись.", len(steps))
	// Typed tools are the authority for targeted checks. A tiny local model can
	// turn successful evidence into a fabricated failure, so do not ask it to
	// reinterpret deterministic results.
	s.state.ModelAnswer = buildTargetedAnswer(steps)
	s.state.Engine = "typed-tools"
	s.state.Messages = appendChatMessage(s.state.Messages, ChatMessage{Role: "assistant", Content: s.state.ModelAnswer, CreatedAt: time.Now()})
}

func appendChatMessage(messages []ChatMessage, message ChatMessage) []ChatMessage {
	message.Content = truncateModelText(sanitizeModelText(strings.TrimSpace(message.Content)), 2000)
	if message.Content == "" {
		return messages
	}
	messages = append(messages, message)
	const maxMessages = 12
	if len(messages) > maxMessages {
		messages = append([]ChatMessage(nil), messages[len(messages)-maxMessages:]...)
	}
	return messages
}

func buildTargetedAnswer(steps []ToolStep) string {
	if len(steps) == 0 {
		return "Адресные проверки не выполнялись."
	}
	var builder strings.Builder
	for i, step := range steps {
		if i > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString(step.Summary)
		if len(step.Evidence) > 0 {
			builder.WriteString(":\n")
			for _, evidence := range step.Evidence {
				builder.WriteString("- ")
				builder.WriteString(evidence)
				builder.WriteByte('\n')
			}
		}
	}
	return strings.TrimSpace(builder.String())
}

func (s *Service) runModel(report diagnostics.Report, findings []Finding, toolSteps []ToolStep) (string, error, string) {
	s.mu.RLock()
	configStore, model, embedded, question := s.config, s.model, s.embedded, s.state.Question
	messages := append([]ChatMessage(nil), s.state.Messages...)
	s.mu.RUnlock()
	if configStore == nil || model == nil {
		return "", nil, ""
	}
	cfg := configStore.Get()
	if !cfg.Enabled {
		return "", nil, ""
	}
	s.mu.Lock()
	s.state.Progress = "Модель анализирует обезличенные результаты…"
	s.mu.Unlock()

	timeout := 60 * time.Second
	switch cfg.Provider {
	case "local_embedded":
		timeout = 240 * time.Second
	case "google":
		timeout = 180 * time.Second
	case "ollama":
		timeout = 180 * time.Second
	case "custom":
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if cfg.Provider == "local_embedded" && embedded != nil {
		if err := embedded.EnsureRunning(ctx); err != nil {
			return "", fmt.Errorf("local model engine: %w", err), cfg.Provider
		}
	}

	payload, err := buildModelDiagnostic(report, findings, toolSteps)
	if err != nil {
		return "", err, cfg.Provider
	}
	ans, modelErr := model.Analyze(ctx, cfg, conversationalQuestion(question, messages), payload)
	return ans, modelErr, cfg.Provider
}

func conversationalQuestion(current string, messages []ChatMessage) string {
	if len(messages) <= 1 {
		return current
	}
	start := len(messages) - 7
	if start < 0 {
		start = 0
	}
	var b strings.Builder
	b.WriteString("Краткая история диалога:\n")
	for _, message := range messages[start : len(messages)-1] {
		if message.Role != "user" && message.Role != "assistant" {
			continue
		}
		b.WriteString(message.Role)
		b.WriteString(": ")
		b.WriteString(truncateModelText(sanitizeModelText(message.Content), 500))
		b.WriteByte('\n')
	}
	b.WriteString("Текущий вопрос: ")
	b.WriteString(current)
	return b.String()
}

func buildModelDiagnostic(report diagnostics.Report, findings []Finding, toolSteps []ToolStep) ([]byte, error) {
	type compactCheck struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Detail string `json:"detail,omitempty"`
	}
	checks := make([]compactCheck, 0, len(report.Tests))
	for _, t := range report.Tests {
		c := compactCheck{Name: t.Name, Status: string(t.Status)}
		if t.Status != "pass" && t.Detail != "" {
			detail := sanitizeModelText(t.Detail)
			detail = truncateModelText(detail, 120)
			c.Detail = detail
		}
		checks = append(checks, c)
	}

	modelFindings := make([]Finding, 0, len(findings))
	for _, f := range findings {
		modelFindings = append(modelFindings, Finding{
			Severity:       f.Severity,
			Title:          sanitizeModelText(f.Title),
			Detail:         sanitizeModelText(f.Detail),
			Recommendation: f.Recommendation,
			Source:         f.Source,
		})
	}

	payload := struct {
		System struct {
			Arch          string `json:"arch"`
			MemoryMB      int    `json:"memoryMB"`
			TotalChecks   int    `json:"totalChecks"`
			RoutingEngine string `json:"routingEngine,omitempty"`
		} `json:"system"`
		Checks   []compactCheck `json:"checks"`
		Findings []Finding      `json:"findings,omitempty"`
		Tools    []ToolStep     `json:"targetedTools,omitempty"`
	}{
		System: struct {
			Arch          string `json:"arch"`
			MemoryMB      int    `json:"memoryMB"`
			TotalChecks   int    `json:"totalChecks"`
			RoutingEngine string `json:"routingEngine,omitempty"`
		}{
			Arch:          report.System.Arch,
			MemoryMB:      report.System.TotalMemoryMB,
			TotalChecks:   len(report.Tests),
			RoutingEngine: report.System.RoutingEngine,
		},
		Checks:   checks,
		Findings: modelFindings,
		Tools:    sanitizeToolSteps(toolSteps),
	}
	return json.Marshal(payload)
}

func sanitizeToolSteps(steps []ToolStep) []ToolStep {
	result := make([]ToolStep, len(steps))
	for i, step := range steps {
		result[i] = step
		result[i].Summary = sanitizeModelText(step.Summary)
		result[i].Evidence = make([]string, len(step.Evidence))
		for j, evidence := range step.Evidence {
			result[i].Evidence[j] = sanitizeModelText(evidence)
		}
	}
	return result
}

var (
	modelIPv4Pattern   = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	modelIPv6Pattern   = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){2,}[0-9a-f:]{0,39}\b`)
	modelAPIKeyPattern = regexp.MustCompile(`(?i)\b(?:sk-[a-z0-9_-]{8,}|bearer\s+[a-z0-9._~+/-]{8,}|api[_-]?key\s*[:=]\s*\S+)`)
	modelWGKeyPattern  = regexp.MustCompile(`\b[A-Za-z0-9+/]{42,44}={0,2}\b`)
)

func sanitizeModelText(value string) string {
	value = modelIPv4Pattern.ReplaceAllString(value, "PRIVATE-IP")
	value = modelIPv6Pattern.ReplaceAllString(value, "PRIVATE-IPV6")
	value = modelAPIKeyPattern.ReplaceAllString(value, "[SECRET]")
	return modelWGKeyPattern.ReplaceAllString(value, "[KEY]")
}

func truncateModelText(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "..."
}

func (s *Service) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Status = "error"
	s.state.Progress = "Диагностика не завершена"
	s.state.CompletedAt = time.Now()
	s.state.Error = err.Error()
	s.state.Messages = appendChatMessage(s.state.Messages, ChatMessage{Role: "assistant", Content: "Не удалось завершить диагностику: " + err.Error(), CreatedAt: time.Now()})
}

func analyze(report diagnostics.Report) (Stats, []Finding) {
	stats := Stats{}
	findings := make([]Finding, 0)
	for _, test := range report.Tests {
		switch test.Status {
		case diagnostics.StatusPass:
			stats.Passed++
		case diagnostics.StatusSkip:
			stats.Skipped++
		case diagnostics.StatusFail, diagnostics.StatusError:
			stats.Failed++
			findings = append(findings, findingForTest(test))
		}
	}

	for _, issue := range report.BootHealth.NotStartedOnBoot {
		findings = append(findings, Finding{
			Severity:       "warning",
			Title:          "Туннель не запустился после загрузки: " + issue.TunnelName,
			Detail:         "Туннель включён и ожидается запущенным, но активного интерфейса нет.",
			Recommendation: "Проверьте автозапуск туннеля и журнал загрузки. Перед перезапуском сохраните текущую конфигурацию.",
			Source:         "boot_health",
		})
	}

	if findings == nil {
		findings = []Finding{}
	}
	return stats, findings
}

func findingForTest(test diagnostics.TestResult) Finding {
	severity := "warning"
	if test.Status == diagnostics.StatusError || isCriticalTest(test.Name) {
		severity = "critical"
	}
	return Finding{
		Severity:       severity,
		Title:          test.Description,
		Detail:         test.Detail,
		Recommendation: recommendation(test.Name),
		Source:         test.Name,
	}
}

func isCriticalTest(name string) bool {
	switch name {
	case "wan_connectivity", "ndms_health", "direct_connectivity", "singbox_runtime", "interface_state_consistency":
		return true
	default:
		return false
	}
}

func recommendation(name string) string {
	switch name {
	case "wan_connectivity":
		return "Проверьте состояние провайдера и наличие default route. Не меняйте policy routing, пока базовый WAN не восстановлен."
	case "ndms_health":
		return "Проверьте доступность NDMS и журнал запуска AWG Manager."
	case "direct_connectivity":
		return "Сначала восстановите прямой доступ с роутера, затем проверяйте прокси-маршруты."
	case "singbox_runtime":
		return "Проверьте выбранный движок, его конфигурацию и последние ошибки запуска."
	case "endpoint_route_check", "route_leak_check":
		return "Сопоставьте ip rule, таблицы маршрутизации и интерфейс выхода. Изменения следует применять по одному с возможностью отката."
	case "firewall_rules", "rp_filter":
		return "Проверьте правила маркировки/перехвата и rp_filter для задействованных интерфейсов."
	case "dns_resolve", "dns_leak_check":
		return "Проверьте DNS-маршрут, перехват порта 53 и доступность настроенных upstream-серверов."
	case "awg_handshake", "endpoint_reachable", "tunnel_connectivity":
		return "Проверьте маршрут до endpoint, время на роутере, ключи и доступность удалённого сервера."
	default:
		return "Откройте подробности проверки и журнал. До подтверждения причины не применяйте автоматические изменения."
	}
}

func buildSummary(stats Stats, findings int) string {
	if findings == 0 {
		return fmt.Sprintf("Проверки завершены: %d успешно, критических отклонений не обнаружено.", stats.Passed)
	}
	return fmt.Sprintf("Обнаружено отклонений: %d. Успешно: %d, пропущено: %d. Исправления не применялись.", findings, stats.Passed, stats.Skipped)
}
