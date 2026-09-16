package wdttlink

import (
	"context"
	"errors"
	"strings"

	"github.com/hoaxisr/awg-manager/internal/proxyrt/instancestore"
)

// LinkRequest — тело ручки ссылки, СУПЕРСЕТ полей обоих старых DTO. json-имена
// вербатим: `peer`/`vkHashes`/`name`/`password` — со старого
// api.WdttGenerateLinkRequest (wdtt_server.go:106-111);
// `provider`/`mtu`/`wg`/`clientId`/`n`/`streamsPerCred`/`transport`/`serverId` —
// со старого api.GenerateLinkRequest freeturn (freeturn.go:249-258; их читает
// реализация freeturn); `name` общее для обоих.
//
// Mode — единственное НОВОЕ поле (§11): режим ссылки wg|raw, независимый от
// RelayMode записи. Пусто — режим записи.
type LinkRequest struct {
	Peer     string   `json:"peer,omitempty"`
	VKHashes []string `json:"vkHashes,omitempty"`
	Name     string   `json:"name,omitempty"`
	Password string   `json:"password,omitempty"`
	Mode     string   `json:"mode,omitempty"`

	Provider       string `json:"provider,omitempty"`
	MTU            int    `json:"mtu,omitempty"`
	WG             string `json:"wg,omitempty"`
	ClientID       string `json:"clientId,omitempty"`
	N              int    `json:"n,omitempty"`
	StreamsPerCred int    `json:"streamsPerCred,omitempty"`
	Transport      string `json:"transport,omitempty"`
	ServerID       string `json:"serverId,omitempty"`
}

// LinkBuilder — сборщик ссылки для ОДНОЙ роли. Ручка ссылки одна на все роли
// (шов Г-8 п. 1): диспетчер по rec.Kind собирает проводка, реализация freeturn
// живёт в своём пакете. Возвращаемое тело — форма ответа своей подсистемы
// (у wdtt: {link, linkQwdtt, peer}), поэтому any, а не общий тип: сводить
// разные формы к одной значило бы менять контракт фронта.
type LinkBuilder interface {
	BuildLink(ctx context.Context, rec instancestore.Record, req LinkRequest) (any, error)
}

// LinkError — отказ сборки ссылки со СВОИМ кодом ответа. Коды — вербатим
// старые: фронт и пользователь видят прежние тексты и коды.
type LinkError struct {
	Code string
	Msg  string
}

func (e *LinkError) Error() string { return e.Msg }

// UnusableReason — почему wdtt-server не примет пароль абонента. Значения —
// вербатим строки старого wdtt.ServerClientReason (passwords_json.go:189-196),
// чтобы прод-реализация переводила их без карты соответствий.
type UnusableReason string

const (
	ReasonUsable        UnusableReason = ""
	ReasonEmptyPassword UnusableReason = "empty_password"
)

// UserVetting — предикат пригодности абонента сервера. Прод-реализация живёт
// там же, где перенесённый passwords_json.go (proxyapp/wdttusers): предикат
// ОДИН на всех потребителей — по нему абоненты уезжают в passwords.json, по
// нему же выдаётся ссылка. Своей копии правила здесь быть не должно: проверка
// по всему списку была бы мягче, и ссылка на просроченного абонента
// собралась бы без единой жалобы и молча не подключилась.
type UserVetting interface {
	UsableUsers(users []instancestore.ServerUser) []instancestore.ServerUser
	UnusableReason(u instancestore.ServerUser) UnusableReason
}

// BuilderDeps — зависимости сборщика ссылок wdtt-сервера.
type BuilderDeps struct {
	// Vetting обязателен: без него пригодность абонента не проверить, и
	// ссылка выдавалась бы на любой пароль. Отсутствие — отказ, не пропуск.
	Vetting UserVetting
	// Mutator — персист адреса последней ссылки (Record.LinkPeer).
	Mutator Mutator
	// ExternalIP — внешний адрес роутера, когда peer не задан ни запросом,
	// ни записью.
	ExternalIP func(ctx context.Context) (string, error)
}

// Builder — ссылки wdtt-сервера (wdtt:// для роутера, qwdtt:// для телефона).
type Builder struct{ deps BuilderDeps }

func NewBuilder(d BuilderDeps) *Builder { return &Builder{deps: d} }

// BuildLink собирает пару ссылок абоненту. Порядок и тексты отказов —
// перенос generateLinkCore (api/wdtt_server.go:441-494).
func (b *Builder) BuildLink(ctx context.Context, rec instancestore.Record, req LinkRequest) (any, error) {
	cfg, err := rec.WdttServerConfig()
	if err != nil {
		return nil, &LinkError{Code: "WDTT_SERVER_NOT_FOUND", Msg: err.Error()}
	}
	// Пароля владельца сборка не спрашивает и не проверяет: ссылка выдаётся на
	// пароль АБОНЕНТА (linkPasswordFor). Прежняя проверка на непустоту
	// владельческого осталась от старой модели и с уходом пароля из UI
	// (решение владельца 2026-08-28) стала неснимаемым отказом: задать его
	// негде, а ссылку не выдать.
	linkPassword, err := b.linkPasswordFor(req, rec)
	if err != nil {
		return nil, &LinkError{Code: "WDTT_LINK_NO_CLIENT", Msg: err.Error()}
	}

	// §11: режим ссылки задаёт запрос; пусто — режим записи. От режима зависит
	// порт в peer и пометка mode= в ссылке.
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = cfg.RelayMode
	}
	mode = normalizeConnMode(mode)
	linkPort := LinkListenPortForMode(cfg, mode)

	// Адрес: запрос → память записи (LinkPeer) → внешний IP роутера.
	peer := strings.TrimSpace(req.Peer)
	if peer == "" {
		peer = strings.TrimSpace(rec.LinkPeer)
	}
	if peer == "" {
		ip, ipErr := b.externalIP(ctx)
		if ipErr != nil {
			return nil, &LinkError{Code: "WDTT_EXTERNAL_IP_FAILED",
				Msg: "Не удалось определить внешний IP: " + ipErr.Error() + ". Укажите peer вручную."}
		}
		peer = ip
	}
	// LinkPeer remembers the last generated endpoint. Always replace its port:
	// otherwise switching a link from WG to RAW keeps the old DTLS/direct port.
	peer = peerWithPort(peer, linkPort)

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Router WDTT"
	}

	hashes := req.VKHashes
	if len(hashes) == 0 {
		hashes = splitHashes(rec.LinkVKHashes)
	}
	// Ссылка без хешей собирается синтаксически верной, но у абонента не
	// работает: транспорт wdtt держится на звонках VK. Молчать здесь значит
	// отдать нерабочую ссылку тому, кто узнает об этом последним — абоненту.
	if len(splitHashes(strings.Join(hashes, ","))) == 0 {
		return nil, &LinkError{Code: "WDTT_LINK_NO_VK_HASHES",
			Msg: "укажите VK-хеши: без них ссылка не заработает"}
	}

	// Оба порта всегда идут в ссылке: сервер слушает dtls и raw одновременно.
	// Клиент выбирает режим по полю mode. Старые клиенты (без знания raw) просто
	// подключатся по dtls-порту, как раньше.
	dtlsPort, listenErr := listenPort(cfg.Listen)
	if listenErr != nil {
		dtlsPort = 56002
	}
	rawPort := LinkListenPortForMode(cfg, ConnModeRaw)
	link, err := EncodeLink(peer, cfg.WgPort, linkPassword, hashes, name)
	if err != nil {
		return nil, &LinkError{Code: "WDTT_LINK_ENCODE_FAILED", Msg: err.Error()}
	}
	qLink, err := EncodeQwdttLinkFull(peer, linkPassword, hashes, name, 0, 0, mode, dtlsPort, rawPort)
	if err != nil {
		return nil, &LinkError{Code: "WDTT_LINK_ENCODE_FAILED", Msg: err.Error()}
	}

	b.persistLinkParams(ctx, rec, peer, hashes)

	return map[string]string{
		"link":      link,
		"linkQwdtt": qLink,
		"peer":      peer,
	}, nil
}

func (b *Builder) externalIP(ctx context.Context) (string, error) {
	if b.deps.ExternalIP == nil {
		return "", errors.New("определение внешнего адреса не подключено")
	}
	return b.deps.ExternalIP(ctx)
}

// persistLinkParams запоминает адрес и хеши последней ссылки В ЗАПИСИ, чтобы ссылка
// восстанавливалась без повторного ввода.
func (b *Builder) persistLinkParams(ctx context.Context, rec instancestore.Record, peer string, hashes []string) {
	if b.deps.Mutator == nil {
		return
	}
	cfg, _ := rec.WdttServerConfig()
	peerChanged := peer != "" && peer != strings.TrimSpace(rec.LinkPeer)
	joinedHashes := strings.Join(hashes, ",")
	hashesChanged := (cfg.ClientAuthMode == "shared" || strings.TrimSpace(rec.LinkVKHashes) == "") &&
		joinedHashes != "" && joinedHashes != strings.TrimSpace(rec.LinkVKHashes)

	if !peerChanged && !hashesChanged {
		return
	}
	_ = b.deps.Mutator.Update(ctx, rec.Key(), func(r *instancestore.Record) error {
		if peerChanged {
			r.LinkPeer = peer
		}
		if hashesChanged {
			r.LinkVKHashes = joinedHashes
		}
		return nil
	})
}

// linkPasswordFor выбирает пароль ссылки. Главный пароль сервера — ключ
// администрирования (X-Admin-Password у admin-API форка), в ссылку он не
// попадает ни при каких условиях, поэтому пароль обязан принадлежать списку
// абонентов сервера.
//
// Членство считается по UserVetting — ровно по тому предикату, по которому
// абоненты уезжают в passwords.json и по которому сервер собирает wrap-ключи.
// Проверка по всему списку записи была бы мягче: ссылка на просроченного
// абонента собралась бы без единой жалобы и молча не подключилась.
func (b *Builder) linkPasswordFor(req LinkRequest, rec instancestore.Record) (string, error) {
	if cfg, err := rec.WdttServerConfig(); err == nil && strings.TrimSpace(cfg.ClientAuthMode) == "shared" {
		pass := strings.TrimSpace(req.Password)
		if pass == "" {
			pass = strings.TrimSpace(cfg.SharedPassword)
		}
		if pass == "" {
			return "", errors.New("не задан общий пароль сервера: укажите пароль в настройках раздачи")
		}
		return pass, nil
	}

	if b.deps.Vetting == nil {
		// Fail-closed: без предиката пригодность абонента не проверить, а
		// выдать ссылку «на всякий пароль» хуже отказа.
		return "", errors.New("проверка абонентов не подключена")
	}
	usable := b.deps.Vetting.UsableUsers(rec.Users)
	if len(usable) == 0 {
		return "", errors.New("у сервера нет ни одного рабочего абонента: заведите абонента и повторите")
	}

	pass := strings.TrimSpace(req.Password)
	if pass == "" {
		return "", errors.New("выберите абонента: ссылка выдаётся на пароль абонента")
	}
	for _, u := range usable {
		// Пароль из UsableUsers уже подрезан — трим тут не нужен.
		if u.Password == pass {
			return pass, nil
		}
	}

	// Пароль не рабочий. Причину спрашиваем у классификатора — того же, на
	// котором построен предикат, а не выводим её исключением: набор условий
	// пригодности живёт в одном месте и может пополниться.
	known := false
	target := instancestore.ServerUser{Password: pass}
	for _, u := range rec.Users {
		if strings.TrimSpace(u.Password) == pass {
			target, known = u, true
			break
		}
	}
	return "", errors.New(linkRejectMessage(b.deps.Vetting.UnusableReason(target), known))
}

// linkRejectMessage переводит причину непригодности в текст отказа.
// «Пароля нет в списке» проверяется ПОСЛЕ главного пароля: главный в списке
// абонентов не лежит, а сказать про него надо именно про него.
func linkRejectMessage(reason UnusableReason, knownClient bool) string {
	switch {
	case !knownClient:
		return "пароль не принадлежит ни одному абоненту сервера"
	default:
		// Причина, которой у текстов ещё нет: новое условие пригодности.
		// Общий отказ честнее уверенного «просрочен».
		return "абонент непригоден для ссылки: заведите нового абонента"
	}
}
