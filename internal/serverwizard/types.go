package serverwizard

import (
	"errors"
	"time"
)

// Standard error codes for API responses.
const (
	ErrCodePlanStale           = "PLAN_STALE"
	ErrCodeRecoveryRequired    = "RECOVERY_REQUIRED"
	ErrCodePortConflict        = "PORT_CONFLICT"
	ErrCodeEgressUnavailable   = "EGRESS_UNAVAILABLE"
	ErrCodeOperationInProgress = "OPERATION_IN_PROGRESS"
	ErrCodePlanNotFound        = "PLAN_NOT_FOUND"
	ErrCodePlanAlreadyUsed     = "PLAN_ALREADY_USED"
	ErrCodePlanExpired         = "PLAN_EXPIRED"
	ErrCodeUnauthorized        = "UNAUTHORIZED"
	ErrCodeCSRFInvalid         = "CSRF_INVALID"
	ErrCodeInvalidRequest      = "INVALID_REQUEST"
	ErrCodeRateLimited         = "RATE_LIMITED"
)

var (
	ErrPlanStale           = errors.New("plan is stale: system state has changed")
	ErrPlanNotFound        = errors.New("plan not found")
	ErrPlanAlreadyUsed     = errors.New("plan has already been used")
	ErrPlanExpired         = errors.New("plan has expired")
	ErrPortConflict        = errors.New("port conflict detected")
	ErrEgressUnavailable   = errors.New("selected egress is unavailable")
	ErrOperationInProgress = errors.New("operation already in progress")
	ErrRecoveryRequired    = errors.New("system in recovery_required state: manual intervention required")
	ErrUnauthorized        = errors.New("unauthorized")
	ErrInvalidRequest      = errors.New("invalid request")
	ErrPlanInvalidState    = errors.New("plan is not in reserved state")
)

type CheckStatus string

const (
	CheckStatusOK      CheckStatus = "ok"
	CheckStatusWarning CheckStatus = "warning"
	CheckStatusBlocked CheckStatus = "blocked"
)

// PreflightCheck represents a single health or validation check before configuration.
type PreflightCheck struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Status      CheckStatus `json:"status"` // "ok", "warning", "blocked"
	Message     string      `json:"message"`
	Details     string      `json:"details,omitempty"`
	Remediation string      `json:"remediation,omitempty"`
}

// PreflightResponse is returned by preflight validation.
type PreflightResponse struct {
	CanProceed  bool             `json:"can_proceed"`
	HasWarnings bool             `json:"has_warnings"`
	Checks      []PreflightCheck `json:"checks"`
	Fingerprint string           `json:"fingerprint"`
}

// PlanItem describes a single discrete action within a ChangePlan.
type PlanItem struct {
	Action      string `json:"action"` // "create", "modify", "enable", "disable", "keep"
	Target      string `json:"target"`
	Description string `json:"description"`
	OldValue    string `json:"old_value,omitempty"`
	NewValue    string `json:"new_value,omitempty"`
}

// ChangePlan encapsulates proposed changes and the state fingerprint when planned.
type ChangePlan struct {
	Summary          string     `json:"summary"`
	Items            []PlanItem `json:"items"`
	RestartRequired  bool       `json:"restart_required"`
	StateFingerprint string     `json:"state_fingerprint"`
}

// WizardPlanRequest is the client submission describing desired wizard setup.
type WizardPlanRequest struct {
	Kind           string `json:"kind"`                      // "tgwebproxy" or "xray"
	Scenario       string `json:"scenario,omitempty"`        // for tg: "direct_fake_tls", "cdn_http", "dual"
	DeviceType     string `json:"device_type,omitempty"`     // for xray: "phone", "pc", "router"
	Mode           string `json:"mode,omitempty"`            // for xray: "xhttp_get", "ws", "split_http"
	PublicDomain   string `json:"public_domain,omitempty"`   // e.g. "cdn.example.com"
	PublicPort     int    `json:"public_port,omitempty"`     // e.g. 443
	Path           string `json:"path,omitempty"`            // e.g. "/cdn-bridge/"
	DispatcherPort int    `json:"dispatcher_port,omitempty"` // e.g. 9009
	ListenPort     int    `json:"listen_port,omitempty"`     // e.g. 9008, 8085
	DirectHost     string `json:"direct_host,omitempty"`     // e.g. "router.crazedns.ru"
	DirectPort     int    `json:"direct_port,omitempty"`     // e.g. 8443
	TlsDomain      string `json:"tls_domain,omitempty"`      // e.g. "gateway.icloud.com"
	UpstreamDevice string `json:"upstream_device,omitempty"` // e.g. "nwg1", "awgm0"
	ClientRemark   string `json:"client_remark,omitempty"`   // e.g. "Дача (Keenetic)", "Телефон (Happ)"
	CdnProfileID   string `json:"cdn_profile_id,omitempty"`  // e.g. "cdn_get", "cdn_ws", "direct"
}

type PlanState string

const (
	PlanStateAvailable PlanState = "available"
	PlanStateReserved  PlanState = "reserved"
	PlanStateConsumed  PlanState = "consumed"
)

// ServerPlanRecord is stored on the server with TTL to prevent client plan tampering.
type ServerPlanRecord struct {
	PlanID           string              `json:"plan_id"`
	Kind             string              `json:"kind"`
	SessionID        string              `json:"session_id"`
	Request          WizardPlanRequest   `json:"request"`
	Desired          DesiredWizardConfig `json:"desired"`
	Plan             ChangePlan          `json:"plan"`
	StateFingerprint string              `json:"state_fingerprint"`
	ExpiresAt        time.Time           `json:"expires_at"`
	State            PlanState           `json:"state"`
	ReservedJobID    string              `json:"reserved_job_id,omitempty"`
	ConsumedJobID    string              `json:"consumed_job_id,omitempty"`
	Used             bool                `json:"used"`
	CreatedAt        time.Time           `json:"created_at"`
}

// JobPhase defines the lifecycle stage of an asynchronous apply job.
type JobPhase string

const (
	JobPhasePending          JobPhase = "pending"
	JobPhasePreparing        JobPhase = "preparing"
	JobPhaseApplying         JobPhase = "applying"
	JobPhaseVerifying        JobPhase = "verifying"
	JobPhaseCommitting       JobPhase = "committing"
	JobPhaseSucceeded        JobPhase = "succeeded"
	JobPhaseCancelling       JobPhase = "cancelling"
	JobPhaseRollingBack      JobPhase = "rolling_back"
	JobPhaseCancelled        JobPhase = "cancelled"
	JobPhaseFailed           JobPhase = "failed"
	JobPhaseRecoveryRequired JobPhase = "recovery_required"
)

// JobStatusResponse is returned by safe public job polling. NO secrets are contained.
type JobStatusResponse struct {
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	Phase           JobPhase  `json:"phase"`
	Progress        int       `json:"progress"` // 0 - 100
	CurrentStep     string    `json:"current_step"`
	Error           string    `json:"error,omitempty"`
	ErrorCode       string    `json:"error_code,omitempty"`
	ResultAvailable bool      `json:"result_available"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// RevealCredentials stores credentials revealed ONLY via authenticated, rate-limited endpoint.
type RevealCredentials struct {
	Kind        string `json:"kind"` // "tgwebproxy" or "xray"
	VlessURL    string `json:"vless_url,omitempty"`
	HappJSON    string `json:"happ_json,omitempty"`
	SingboxJSON string `json:"singbox_json,omitempty"`
	MihomoYAML  string `json:"mihomo_yaml,omitempty"`
	UUID        string `json:"uuid,omitempty"`
	Remark      string `json:"remark,omitempty"`
	DirectLink  string `json:"direct_link,omitempty"`
	WebLink     string `json:"web_link,omitempty"`
	TgSecret    string `json:"tg_secret,omitempty"`
	DirectHost  string `json:"direct_host,omitempty"`
	DirectPort  int    `json:"direct_port,omitempty"`
	PublicHost  string `json:"public_host,omitempty"`
}

// ApplyRequest is the payload sent by the frontend to apply a planned configuration.
type ApplyRequest struct {
	PlanID string `json:"plan_id"`
}
