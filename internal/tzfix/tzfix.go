// Package tzfix переустанавливает time.Local из POSIX-формы переменной TZ.
//
// Причина: awg-manager запускает freeturn с env TZ=<строка из /etc/TZ Keenetic>,
// где строка — POSIX-спека вида "MSK-3" (не IANA-имя "Europe/Moscow"). Go
// time.Local НЕ парсит POSIX-TZ из env: TZ=MSK-3 → time.Local=UTC, и на роутере
// нет zoneinfo-файлов, а embedded-tzdata тоже не помогает (в tzdata нет зоны с
// именем «MSK-3»). Из-за этого штампы stdlib-log отстают на offset зоны.
//
// Apply парсит offset из POSIX-строки и ставит time.Local = FixedZone(...).
// Лимитация: DST-правила игнорируются — берётся только первый (std) offset.
// Для целевых роутеров (Россия без DST) этого достаточно.
package tzfix

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Apply читает env TZ и при успешном POSIX-парсе переустанавливает time.Local.
// Вызывать ПЕРВОЙ строкой main(): time.Local — глобал без синхронизации, а
// после старта горутин/логгера запись в него — гонка.
func Apply() {
	if name, offsetSec, ok := parsePosixOffset(os.Getenv("TZ")); ok {
		time.Local = time.FixedZone(name, offsetSec)
	}
}

// parsePosixOffset разбирает POSIX-форму TZ "std offset[dst[offset]][,rule]" и
// возвращает имя зоны, offset в секундах на восток от UTC (для time.FixedZone) и
// ok. Берётся только первый (std) offset; dst/rule игнорируются.
//
// POSIX-знак offset ИНВЕРТИРОВАН: в POSIX offset — это сколько прибавить к local
// чтобы получить UTC (запад положителен), а FixedZone ждёт секунды на восток.
// Поэтому offsetSec = -posixOffset. Примеры: "MSK-3"→(MSK,+10800), "<+03>-3"→
// (+03,+10800), "EST5"→(EST,-18000), "UTC0"→(UTC,0). IANA-имя/пустое → ok=false.
func parsePosixOffset(tz string) (name string, offsetSec int, ok bool) {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return "", 0, false
	}

	var rest string
	if tz[0] == '<' {
		end := strings.IndexByte(tz, '>')
		if end < 0 {
			return "", 0, false
		}
		name = tz[1:end]
		rest = tz[end+1:]
	} else {
		i := 0
		for i < len(tz) && isAlpha(tz[i]) {
			i++
		}
		if i == 0 {
			return "", 0, false
		}
		name = tz[:i]
		rest = tz[i:]
	}

	posix, ok := parseOffset(rest)
	if !ok {
		return "", 0, false
	}
	return name, -posix, true
}

// parseOffset разбирает POSIX-offset "[+|-]hh[:mm[:ss]]" из начала s; хвост
// (dst-имя, ",rule") игнорируется. Знак POSIX — как в спеке (запад положителен).
func parseOffset(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	sign := 1
	switch s[0] {
	case '+':
		s = s[1:]
	case '-':
		sign = -1
		s = s[1:]
	}

	hh, s, ok := takeInt(s)
	if !ok {
		return 0, false
	}
	mm, ss := 0, 0
	if strings.HasPrefix(s, ":") {
		if mm, s, ok = takeInt(s[1:]); !ok {
			return 0, false
		}
		if strings.HasPrefix(s, ":") {
			if ss, _, ok = takeInt(s[1:]); !ok {
				return 0, false
			}
		}
	}
	return sign * (hh*3600 + mm*60 + ss), true
}

// takeInt откусывает ведущий пробег десятичных цифр и возвращает число, остаток
// и ok (false — если цифр в начале нет).
func takeInt(s string) (int, string, bool) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, s, false
	}
	n, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0, s, false
	}
	return n, s[i:], true
}

func isAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
