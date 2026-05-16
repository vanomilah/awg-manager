package router

type Status struct {
	Enabled                bool    `json:"enabled"`
	Installed              bool    `json:"installed"`
	NetfilterAvailable     bool    `json:"netfilterAvailable"`
	NetfilterComponentName string  `json:"netfilterComponentName,omitempty"`
	TProxyTargetAvailable  bool    `json:"tproxyTargetAvailable"`
	PolicyName             string  `json:"policyName"`
	PolicyMark             string  `json:"policyMark,omitempty"`
	PolicyExists           bool    `json:"policyExists"`
	DeviceCount            int     `json:"deviceCount"`
	RuleCount              int     `json:"ruleCount"`
	RuleSetCount           int     `json:"ruleSetCount"`
	OutboundAWGCount       int     `json:"outboundAwgCount"`
	OutboundCompositeCount int     `json:"outboundCompositeCount"`
	Final                  string  `json:"final"`
	Issues                 []Issue `json:"issues,omitempty"`
}

type Issue struct {
	Severity  string `json:"severity"`
	Kind      string `json:"kind"`
	RuleIndex int    `json:"ruleIndex,omitempty"`
	Tag       string `json:"tag,omitempty"`
	Message   string `json:"message"`
}

type Rule struct {
	// Type+Mode+Rules express a sing-box logical rule (`type:"logical"`,
	// `mode:"or"|"and"`) when set. System hijack-dns uses this form to
	// match either `protocol:dns` (sniffed) or `port:53` (direct) so a
	// LAN client setting an explicit DNS server reaches sing-box's hijack
	// path even when sniffing missed the protocol. Nested entries inside
	// `Rules` have no Action (the parent owns it); Action is omitempty
	// so nested marshaling stays clean.
	Type         string   `json:"type,omitempty"`
	Mode         string   `json:"mode,omitempty"`
	Rules        []Rule   `json:"rules,omitempty"`
	DomainSuffix []string `json:"domain_suffix,omitempty"`
	IPCIDR       []string `json:"ip_cidr,omitempty"`
	SourceIPCIDR []string `json:"source_ip_cidr,omitempty"`
	Port         []int    `json:"port,omitempty"`
	RuleSet      []string `json:"rule_set,omitempty"`
	Protocol     string   `json:"protocol,omitempty"`
	Action       string   `json:"action,omitempty"`
	Outbound     string   `json:"outbound,omitempty"`
}

type RuleSet struct {
	Tag            string           `json:"tag"`
	Type           string           `json:"type"`
	Format         string           `json:"format,omitempty"`
	URL            string           `json:"url,omitempty"`
	UpdateInterval string           `json:"update_interval,omitempty"`
	DownloadDetour string           `json:"download_detour,omitempty"`
	Path           string           `json:"path,omitempty"`
	Rules          []map[string]any `json:"rules,omitempty"`
}

type Outbound struct {
	Type          string   `json:"type"`
	Tag           string   `json:"tag"`
	BindInterface string   `json:"bind_interface,omitempty"`
	Outbounds     []string `json:"outbounds,omitempty"`
	URL           string   `json:"url,omitempty"`
	Interval      string   `json:"interval,omitempty"`
	Tolerance     int      `json:"tolerance,omitempty"`
	Default       string   `json:"default,omitempty"`
	Strategy      string   `json:"strategy,omitempty"`
}

// CompositeOutboundView is the API/list projection of a composite
// outbound — the canonical Outbound plus a Source tag identifying which
// orchestrator slot owns it. "router" entries come from 20-router.json
// (mutable via this service); "subscription" entries come from
// 40-subscriptions.json (managed by the subscription service — the UI
// renders them read-only).
type CompositeOutboundView struct {
	Outbound
	Source string `json:"source"`
}

type Inbound struct {
	Type         string `json:"type"`
	Tag          string `json:"tag"`
	Listen       string `json:"listen"`
	ListenPort   int    `json:"listen_port"`
	Network      string `json:"network,omitempty"`
	UDPTimeout   string `json:"udp_timeout,omitempty"`
	UDPFragment  bool   `json:"udp_fragment,omitempty"`
	TCPFastOpen  bool   `json:"tcp_fast_open,omitempty"`
	RoutingMark  int    `json:"routing_mark,omitempty"`
}

type Route struct {
	RuleSet []RuleSet `json:"rule_set,omitempty"`
	Rules   []Rule    `json:"rules,omitempty"`
	Final   string    `json:"final,omitempty"`
}

type DomainResolver struct {
	Server   string `json:"server"`
	Strategy string `json:"strategy,omitempty"`
}

type DNSServer struct {
	Tag            string          `json:"tag"`
	Type           string          `json:"type"`
	Server         string          `json:"server"`
	ServerPort     int             `json:"server_port,omitempty"`
	Path           string          `json:"path,omitempty"`
	Detour         string          `json:"detour,omitempty"`
	Strategy       string          `json:"domain_strategy,omitempty"`
	DomainResolver *DomainResolver `json:"domain_resolver,omitempty"`
}

type DNSRule struct {
	RuleSet       []string `json:"rule_set,omitempty"`
	DomainSuffix  []string `json:"domain_suffix,omitempty"`
	Domain        []string `json:"domain,omitempty"`
	DomainKeyword []string `json:"domain_keyword,omitempty"`
	QueryType     []string `json:"query_type,omitempty"`
	Server        string   `json:"server,omitempty"`
	Action        string   `json:"action,omitempty"`
}

type DNS struct {
	Servers  []DNSServer `json:"servers,omitempty"`
	Rules    []DNSRule   `json:"rules,omitempty"`
	Final    string      `json:"final,omitempty"`
	Strategy string      `json:"strategy,omitempty"`
}

type RouterConfig struct {
	Inbounds  []Inbound  `json:"inbounds"`
	Outbounds []Outbound `json:"outbounds"`
	DNS       DNS        `json:"dns,omitempty"`
	Route     Route      `json:"route"`
}
