package storage

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// InstanceIDFile — случайный ID установки для анонимного счёта установок.
// Экспортировано ради internal/backup: ID привязан к установке и в архив не
// едет — иначе бэкап, развёрнутый на нескольких роутерах, склеил бы их в
// одну установку.
const InstanceIDFile = "instance-id"

// LoadOrCreateInstanceID читает ID установки из dataDir, а если его нет или
// он негоден — заводит новый (16 случайных байт, hex). Никаких данных о
// роутере в ID нет: он только отличает одну установку от другой.
func LoadOrCreateInstanceID(dataDir string) (string, error) {
	path := filepath.Join(dataDir, InstanceIDFile)
	if data, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(data)); validInstanceID(id) {
			return id, nil
		}
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b[:])
	if err := AtomicWrite(path, []byte(id+"\n")); err != nil {
		return "", err
	}
	return id, nil
}

func validInstanceID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
