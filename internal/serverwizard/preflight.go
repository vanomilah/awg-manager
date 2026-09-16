package serverwizard

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/hoaxisr/awg-manager/internal/serverwizard/cdn"
	"github.com/hoaxisr/awg-manager/internal/serverwizard/egress"
	"github.com/hoaxisr/awg-manager/internal/sys/procnet"
)

// PreflightEngine executes non-mutating readiness checks before wizard planning.
type PreflightEngine struct {
	xrayPath   string
	tproxyPath string
	telemtPath string
	procDir    string
	statFn     func(path string) (os.FileInfo, error)
	findProcFn func(procDir, addr string, port int) (procnet.ListenerLookup, error)
	xraySvc    XrayStateReader
	dispSvc    DispatcherStateReader
	tgSvc      TgWebProxyStateReader
	recReader  RecoveryStateReader
	egressAdp  *egress.Adapter
}

func NewPreflightEngine(
	xray XrayStateReader,
	disp DispatcherStateReader,
	tg TgWebProxyStateReader,
	rec RecoveryStateReader,
	egressAdp *egress.Adapter,
) *PreflightEngine {
	return &PreflightEngine{
		xrayPath:   "/opt/sbin/xray",
		tproxyPath: "/opt/bin/tproxy-server",
		telemtPath: "/opt/bin/telemt",
		procDir:    "/proc",
		statFn:     os.Stat,
		findProcFn: procnet.FindListeningProcess,
		xraySvc:    xray,
		dispSvc:    disp,
		tgSvc:      tg,
		recReader:  rec,
		egressAdp:  egressAdp,
	}
}

// SetProcDir overrides procfs root path for testing.
func (pe *PreflightEngine) SetProcDir(dir string) {
	pe.procDir = dir
}

// SetStatFn overrides file stat probe for testing.
func (pe *PreflightEngine) SetStatFn(fn func(path string) (os.FileInfo, error)) {
	pe.statFn = fn
}

// SetFindProcFn overrides socket listener probe for testing.
func (pe *PreflightEngine) SetFindProcFn(fn func(procDir, addr string, port int) (procnet.ListenerLookup, error)) {
	pe.findProcFn = fn
}

// Run executes all checks for the specified wizard request and returns a consolidated report.
func (pe *PreflightEngine) Run(ctx context.Context, req WizardPlanRequest) PreflightResponse {
	var checks []PreflightCheck

	// 1. Check recovery required (fail-fast)
	if pe.recReader != nil {
		reqRec, reason := pe.recReader.IsRecoveryRequired()
		if reqRec {
			checks = append(checks, PreflightCheck{
				ID:          "system_recovery",
				Title:       "Целостность транзакций координатора",
				Status:      CheckStatusBlocked,
				Message:     "Система находится в состоянии требуемого восстановления (recovery_required)",
				Details:     reason,
				Remediation: "Выполните сброс или ручное завершение зависшей транзакции перед изменением конфигурации.",
			})
		} else {
			checks = append(checks, PreflightCheck{
				ID:      "system_recovery",
				Title:   "Целостность транзакций координатора",
				Status:  CheckStatusOK,
				Message: "Состояние транзакций стабильно, откатов не требуется",
			})
		}
	}

	// 2. Kind-specific checks
	switch req.Kind {
	case "tgwebproxy":
		pe.checkTelegram(ctx, req, &checks)
	case "xray":
		pe.checkXray(ctx, req, &checks)
	default:
		checks = append(checks, PreflightCheck{
			ID:          "wizard_kind",
			Title:       "Тип мастера настройки",
			Status:      CheckStatusBlocked,
			Message:     fmt.Sprintf("Неподдерживаемый тип мастера: %s", req.Kind),
			Remediation: "Выберите поддерживаемый тип: 'tgwebproxy' или 'xray'.",
		})
	}

	canProceed := true
	hasWarnings := false
	for _, c := range checks {
		if c.Status == CheckStatusBlocked {
			canProceed = false
		} else if c.Status == CheckStatusWarning {
			hasWarnings = true
		}
	}

	return PreflightResponse{
		CanProceed:  canProceed,
		HasWarnings: hasWarnings,
		Checks:      checks,
	}
}

func (pe *PreflightEngine) checkTelegram(ctx context.Context, req WizardPlanRequest, checks *[]PreflightCheck) {
	// Binary checks
	pe.checkBinary("bin_telemt", "Служба telemt (MTProto Fake-TLS)", pe.telemtPath, checks)
	pe.checkBinary("bin_tproxy", "Служба tproxy-server (Web Proxy)", pe.tproxyPath, checks)

	// Ports checks
	directPort := req.DirectPort
	if directPort <= 0 {
		directPort = 8443
	}
	webPort := req.ListenPort
	if webPort <= 0 {
		webPort = 8085
	}

	var currentWebPID int
	var currentDirectPID int
	if pe.tgSvc != nil {
		st := pe.tgSvc.GetStatus()
		currentWebPID = st.PID
		currentDirectPID = st.DirectPID
	}
	if currentDirectPID == 0 {
		if pidBytes, err := os.ReadFile("/opt/var/run/telemt.pid"); err == nil {
			if p, err := strconv.Atoi(strings.TrimSpace(string(pidBytes))); err == nil {
				currentDirectPID = p
			}
		}
	}

	pe.checkPort("port_direct", fmt.Sprintf("Порт прямого подключения (%d)", directPort), directPort, currentDirectPID, checks)
	pe.checkPort("port_web", fmt.Sprintf("Порт веб-прокси (%d)", webPort), webPort, currentWebPID, checks)

	if req.DirectHost != "" {
		if err := ValidateDirectHost(req.DirectHost); err != nil {
			*checks = append(*checks, PreflightCheck{
				ID:          "direct_host_format",
				Title:       "Формат прямого хоста",
				Status:      CheckStatusBlocked,
				Message:     fmt.Sprintf("Некорректный прямой хост: %v", err),
				Remediation: "Укажите публичный IP адрес роутера или домен (KeenDNS / DynDNS).",
			})
		}
	}
	if req.PublicDomain != "" {
		if err := ValidatePublicHostname(req.PublicDomain); err != nil {
			*checks = append(*checks, PreflightCheck{
				ID:          "public_domain_format",
				Title:       "Формат публичного домена",
				Status:      CheckStatusBlocked,
				Message:     fmt.Sprintf("Некорректный публичный домен: %v", err),
				Remediation: "Укажите корректный FQDN для CDN (например, tg.mydomain.com). Публичный домен не может быть IP-адресом.",
			})
		}
	}

	// CDN Profile check
	if req.CdnProfileID != "" {
		if _, ok := cdn.GetProfile(req.CdnProfileID); !ok {
			*checks = append(*checks, PreflightCheck{
				ID:          "cdn_profile",
				Title:       "Профиль CDN",
				Status:      CheckStatusWarning,
				Message:     fmt.Sprintf("Неизвестный профиль CDN: %s", req.CdnProfileID),
				Remediation: "Выберите стандартный профиль CDN (например, cdn_get или cdn_ws).",
			})
		}
	}

	// Egress upstream check
	if req.UpstreamDevice != "" && pe.egressAdp != nil {
		opt, ok := pe.egressAdp.FindOption(ctx, req.UpstreamDevice)
		if !ok {
			*checks = append(*checks, PreflightCheck{
				ID:          "upstream_device",
				Title:       "Интерфейс выхода в сеть",
				Status:      CheckStatusWarning,
				Message:     fmt.Sprintf("Указанный интерфейс '%s' не найден в каталоге", req.UpstreamDevice),
				Remediation: "Выберите существующий туннель или прямой выход (direct).",
			})
		} else if !opt.Available {
			*checks = append(*checks, PreflightCheck{
				ID:          "upstream_device",
				Title:       "Интерфейс выхода в сеть",
				Status:      CheckStatusWarning,
				Message:     fmt.Sprintf("Интерфейс '%s' сейчас не готов: %s", req.UpstreamDevice, opt.DegradedMsg),
				Remediation: "Убедитесь, что выбранный туннель поднят и работоспособен.",
			})
		} else {
			*checks = append(*checks, PreflightCheck{
				ID:      "upstream_device",
				Title:   "Интерфейс выхода в сеть",
				Status:  CheckStatusOK,
				Message: fmt.Sprintf("Интерфейс '%s' активен и готов маршрутизировать трафик", opt.Name),
			})
		}
	}
}

func (pe *PreflightEngine) checkXray(ctx context.Context, req WizardPlanRequest, checks *[]PreflightCheck) {
	// Binary check
	pe.checkBinary("bin_xray", "Бинарный файл Xray-core", pe.xrayPath, checks)

	// Ports checks
	listenPort := req.ListenPort
	if listenPort <= 0 {
		listenPort = 9008
	}
	dispPort := 9009

	var currentXrayPID int
	if pe.xraySvc != nil {
		currentXrayPID = pe.xraySvc.GetStatus().PID
	}

	var currentDispPID int
	if pe.dispSvc != nil && pe.dispSvc.IsRunning() {
		currentDispPID = os.Getpid()
	}

	pe.checkPort("port_xray", fmt.Sprintf("Локальный порт Xray (: %d)", listenPort), listenPort, currentXrayPID, checks)
	pe.checkPort("port_dispatcher", fmt.Sprintf("Порт CDN-диспетчера (: %d)", dispPort), dispPort, currentDispPID, checks)

	// Domain & Path check
	if req.PublicDomain != "" {
		if err := ValidatePublicHostname(req.PublicDomain); err != nil {
			*checks = append(*checks, PreflightCheck{
				ID:          "domain_format",
				Title:       "Формат публичного домена",
				Status:      CheckStatusBlocked,
				Message:     fmt.Sprintf("Некорректный публичный домен: %v", err),
				Remediation: "Укажите корректный FQDN (например, cdn.mydomain.com). Публичный домен не может быть IP-адресом.",
			})
		} else {
			*checks = append(*checks, PreflightCheck{
				ID:      "domain_format",
				Title:   "Формат публичного домена",
				Status:  CheckStatusOK,
				Message: fmt.Sprintf("Публичный домен %s корректен", req.PublicDomain),
			})
		}
	}

	if req.Path != "" {
		if !strings.HasPrefix(req.Path, "/") {
			*checks = append(*checks, PreflightCheck{
				ID:          "path_format",
				Title:       "Формат пути сервиса",
				Status:      CheckStatusBlocked,
				Message:     "Путь должен начинаться с символа '/'",
				Remediation: "Укажите путь в формате '/cdn-bridge/'.",
			})
		}
	}

	// Outbound routing (Mihomo 1099 check)
	lookup, err := pe.findProcFn(pe.procDir, "0.0.0.0", 1099)
	if err == nil && lookup.SocketFound {
		*checks = append(*checks, PreflightCheck{
			ID:      "mihomo_outbound",
			Title:   "Маршрутизация через Mihomo (:1099)",
			Status:  CheckStatusOK,
			Message: "Входящий mixed-порт Mihomo активен, клиенты Xray смогут использовать правила роутера",
		})
	} else {
		*checks = append(*checks, PreflightCheck{
			ID:          "mihomo_outbound",
			Title:       "Маршрутизация через Mihomo (:1099)",
			Status:      CheckStatusWarning,
			Message:     "Служба Mihomo не слушает порт 1099 или находится в процессе перезапуска",
			Remediation: "Убедитесь, что движок маршрутизации запущен в меню 'Маршрутизация'.",
		})
	}
}

func (pe *PreflightEngine) checkBinary(id, title, path string, checks *[]PreflightCheck) {
	info, err := pe.statFn(path)
	if err != nil {
		*checks = append(*checks, PreflightCheck{
			ID:          id,
			Title:       title,
			Status:      CheckStatusBlocked,
			Message:     fmt.Sprintf("Файл не найден по пути %s", path),
			Details:     err.Error(),
			Remediation: fmt.Sprintf("Установите соответствующий пакет в Entware: %s", path),
		})
		return
	}
	if info.IsDir() {
		*checks = append(*checks, PreflightCheck{
			ID:          id,
			Title:       title,
			Status:      CheckStatusBlocked,
			Message:     fmt.Sprintf("По пути %s обнаружен каталог вместо исполняемого файла", path),
			Remediation: "Удалите каталог и установите исполняемый бинарный файл.",
		})
		return
	}

	*checks = append(*checks, PreflightCheck{
		ID:      id,
		Title:   title,
		Status:  CheckStatusOK,
		Message: fmt.Sprintf("Бинарный файл доступен (%d байт)", info.Size()),
	})
}

func (pe *PreflightEngine) checkPort(id, title string, port int, allowedPID int, checks *[]PreflightCheck) {
	lookup, err := pe.findProcFn(pe.procDir, "0.0.0.0", port)
	if err != nil {
		// Procnet failure must fail closed!
		*checks = append(*checks, PreflightCheck{
			ID:          id,
			Title:       title,
			Status:      CheckStatusBlocked,
			Message:     fmt.Sprintf("Не удалось проверить состояние порта %d в procfs", port),
			Details:     err.Error(),
			Remediation: "Проверьте доступность procfs и права процесса awg-manager.",
		})
		return
	}

	if !lookup.SocketFound {
		*checks = append(*checks, PreflightCheck{
			ID:      id,
			Title:   title,
			Status:  CheckStatusOK,
			Message: fmt.Sprintf("Порт %d свободен", port),
		})
		return
	}

	// Socket is found
	if allowedPID > 0 && lookup.PID == allowedPID {
		*checks = append(*checks, PreflightCheck{
			ID:      id,
			Title:   title,
			Status:  CheckStatusOK,
			Message: fmt.Sprintf("Порт %d занят текущим процессом службы (PID %d) и будет переконфигурирован", port, lookup.PID),
		})
		return
	}

	// Conflict with another process or unresolved PID
	pidInfo := "неизвестный PID"
	if lookup.PID > 0 {
		pidInfo = fmt.Sprintf("PID %d", lookup.PID)
	}
	*checks = append(*checks, PreflightCheck{
		ID:          id,
		Title:       title,
		Status:      CheckStatusBlocked,
		Message:     fmt.Sprintf("Конфликт порта: порт %d уже слушает сторонний процесс (%s)", port, pidInfo),
		Details:     fmt.Sprintf("Socket inode: %s", lookup.SocketInode),
		Remediation: fmt.Sprintf("Остановите конфликтующий процесс (%s) или выберите другой порт.", pidInfo),
	})
}

// Ensure interface compatibility
var _ = net.ParseIP
