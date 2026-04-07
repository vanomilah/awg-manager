#!/bin/sh
# AWG Manager — установщик для роутеров Keenetic
#
# Установка (последняя версия):
#   curl -sL https://raw.githubusercontent.com/hoaxisr/awg-manager/main/scripts/install.sh | sh
#   wget -qO- https://raw.githubusercontent.com/hoaxisr/awg-manager/main/scripts/install.sh | sh
#
# Установка конкретной версии:
#   curl -sL .../install.sh | sh -s -- 2.1.0
#
# Обновление: запустите ту же команду повторно

set -e

ENTWARE_REPO="http://repo.hoaxisr.ru"
OPKG_CONF="/opt/etc/opkg/awg_manager.conf"
TMP_DIR="/tmp/awg-manager-install"

# Очистка при ошибке
cleanup() {
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT

info()  { printf "\033[1;32m[+]\033[0m %s\n" "$1"; }
warn()  { printf "\033[1;33m[!]\033[0m %s\n" "$1"; }
error() { printf "\033[1;31m[-]\033[0m %s\n" "$1"; exit 1; }

# --- Убедиться что curl установлен ---
ensure_curl() {
    if command -v curl >/dev/null 2>&1; then
        return
    fi

    info "curl не найден, устанавливаю..."
    opkg update >/dev/null 2>&1 || error "Не удалось выполнить opkg update"
    opkg install curl >/dev/null 2>&1 || error "Не удалось установить curl"

    if ! command -v curl >/dev/null 2>&1; then
        error "curl установлен, но не найден в PATH"
    fi
    info "curl установлен"
}

# --- Определение архитектуры ---
detect_arch() {
    info "Определяю архитектуру..."
    ARCH=$(opkg print-architecture 2>/dev/null | grep '_kn' | awk '{print $2}' | sed 's/_kn.*//')
    [ -z "$ARCH" ] && error "Не удалось определить архитектуру. Это роутер Keenetic с Entware?"

    case "$ARCH" in
        mipsel-3.4|mips-3.4|aarch64-3.10) ;;
        *) error "Неподдерживаемая архитектура: $ARCH" ;;
    esac

    # Convert filename arch (e.g. aarch64-3.10) to repo dir (aarch64-k3.10)
    REPO_ARCH=$(echo "$ARCH" | sed 's/-\([0-9]\)/-k\1/')

    info "Архитектура: $ARCH (repo: $REPO_ARCH)"
}

# --- Проверка текущей установки ---
check_existing() {
    INSTALLED_VERSION=$(opkg list-installed 2>/dev/null | awk '/^awg-manager /{print $3}')
    if [ -n "$INSTALLED_VERSION" ]; then
        info "Установлена версия: $INSTALLED_VERSION"
        IS_UPDATE=1
    else
        IS_UPDATE=0
    fi
}

# --- Получить последнюю версию из Packages.gz ---
fetch_version() {
    if [ -n "${TARGET_VERSION:-}" ]; then
        VERSION="$TARGET_VERSION"
        info "Запрошена версия: $VERSION"
        return
    fi

    info "Получаю последнюю версию с ${ENTWARE_REPO}/${REPO_ARCH}..."

    PACKAGES_INDEX=$(curl -fsL "${ENTWARE_REPO}/${REPO_ARCH}/Packages.gz" 2>/dev/null | gunzip 2>/dev/null) \
        || error "Не удалось скачать Packages.gz с ${ENTWARE_REPO}/${REPO_ARCH}"

    # Parse all "Package: awg-manager" blocks and pick the highest semver Version.
    # Output: single line "<version> <filename>"
    LATEST=$(printf '%s\n' "$PACKAGES_INDEX" | awk '
        function vercmp(a, b,    pa, pb, na, nb, i, m, x, y) {
            na = split(a, pa, ".")
            nb = split(b, pb, ".")
            m = (na > nb) ? na : nb
            for (i = 1; i <= m; i++) {
                x = (i <= na) ? pa[i] + 0 : 0
                y = (i <= nb) ? pb[i] + 0 : 0
                if (x < y) return -1
                if (x > y) return 1
            }
            return 0
        }
        BEGIN { RS=""; FS="\n"; bestv=""; bestf="" }
        {
            pkg=""; ver=""; fn=""
            for (i = 1; i <= NF; i++) {
                if ($i ~ /^Package: /)  pkg = substr($i, 10)
                if ($i ~ /^Version: /)  ver = substr($i, 10)
                if ($i ~ /^Filename: /) fn  = substr($i, 11)
            }
            if (pkg == "awg-manager" && (bestv == "" || vercmp(ver, bestv) > 0)) {
                bestv = ver; bestf = fn
            }
        }
        END { if (bestv != "") print bestv " " bestf }
    ')

    [ -z "$LATEST" ] && error "Пакет awg-manager не найден в ${ENTWARE_REPO}/${REPO_ARCH}/Packages.gz"

    VERSION=$(echo "$LATEST" | awk '{print $1}')
    PKG_FILENAME=$(echo "$LATEST" | awk '{print $2}')

    info "Последняя версия: $VERSION"
}

# --- Скачать и установить ---
install_package() {
    # When a specific version is requested, derive the filename ourselves.
    # When fetch_version parsed Packages.gz, PKG_FILENAME is already set.
    if [ -z "${PKG_FILENAME:-}" ]; then
        PKG_FILENAME="awg-manager_${VERSION}_${ARCH}-kn.ipk"
    fi

    URL="${ENTWARE_REPO}/${REPO_ARCH}/${PKG_FILENAME}"

    mkdir -p "$TMP_DIR"

    if [ "$IS_UPDATE" = "1" ] && [ "$INSTALLED_VERSION" = "$VERSION" ]; then
        warn "Версия $VERSION уже установлена"
        printf "Переустановить? [y/N] "
        # stdin может быть занят пайпом (curl | sh), читаем из /dev/tty
        answer=""
        read -r answer < /dev/tty 2>/dev/null || answer="y"
        case "$answer" in
            [Yy]*) ;;
            *) info "Отменено"; exit 0 ;;
        esac
    fi

    if [ "$IS_UPDATE" = "1" ]; then
        info "Обновляю: $INSTALLED_VERSION -> $VERSION"
    else
        info "Устанавливаю версию $VERSION"
    fi

    info "Скачиваю $PKG_FILENAME..."
    curl -fL -o "$TMP_DIR/$PKG_FILENAME" "$URL" \
        || error "Ошибка загрузки. Проверьте, что версия $VERSION существует: $URL"

    info "Устанавливаю пакет..."
    opkg install "$TMP_DIR/$PKG_FILENAME" \
        || error "Ошибка установки пакета"

    info "Пакет установлен"
}

# --- Добавить opkg репозиторий ---
add_repo() {
    REPO_LINE="src/gz hoaxisr ${ENTWARE_REPO}/${REPO_ARCH}"

    if [ -f "$OPKG_CONF" ] && grep -qF "$REPO_LINE" "$OPKG_CONF" 2>/dev/null; then
        return
    fi

    mkdir -p /opt/etc/opkg
    echo "$REPO_LINE" > "$OPKG_CONF"
    info "Репозиторий добавлен: ${ENTWARE_REPO}/${REPO_ARCH}"
}

# --- Запуск сервиса ---
start_service() {
    info "Запускаю сервис..."
    /opt/etc/init.d/S99awg-manager restart 2>/dev/null \
        || /opt/bin/awg-manager --service start 2>/dev/null \
        || warn "Не удалось запустить автоматически. Запустите вручную: /opt/etc/init.d/S99awg-manager start"
}

# --- Проверка работоспособности ---
health_check() {
    # Daemon persists actual port in settings.json (fallback port included)
    PORT=$(sed -n 's/.*"port"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p' \
        /opt/etc/awg-manager/settings.json 2>/dev/null)
    [ -z "$PORT" ] && PORT=2222

    info "Проверяю работоспособность (порт $PORT)..."

    attempts=0
    max_attempts=3
    while [ "$attempts" -lt "$max_attempts" ]; do
        attempts=$((attempts + 1))
        if curl -sf "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1; then
            info "Сервис работает!"
            return 0
        fi
        [ "$attempts" -lt "$max_attempts" ] && sleep 2
    done

    warn "Сервис не отвечает на порту $PORT (может потребоваться больше времени для запуска)"
}

# --- Показать URL доступа ---
show_access_url() {
    PORT=$(sed -n 's/.*"port"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p' \
        /opt/etc/awg-manager/settings.json 2>/dev/null)
    [ -z "$PORT" ] && PORT=2222

    IP=$(ip -4 addr show br0 2>/dev/null | awk '/inet /{sub(/\/.*/, "", $2); print $2; exit}')
    [ -z "$IP" ] && IP="192.168.1.1"

    echo ""
    info "========================================"
    if [ "$IS_UPDATE" = "1" ]; then
        info "  Обновление завершено!"
    else
        info "  Установка завершена!"
    fi
    info "  AWG Manager: http://${IP}:${PORT}"
    info "========================================"
    echo ""
}

# --- Main ---
TARGET_VERSION="${1:-}"

ensure_curl
detect_arch
add_repo
check_existing
fetch_version
install_package
start_service
health_check
show_access_url
