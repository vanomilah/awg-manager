package xrayserver

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ListenerOwnershipProbe provides process-to-listening-socket verification
type ListenerOwnershipProbe interface {
	IsAddressPortOwnedByPID(pid int, address string, port int) (bool, error)
	GetAddressPortOwnerPID(address string, port int) (int, error)
}

// DefaultListenerProbe is the standard platform-aware probe
type DefaultListenerProbe struct{}

func NewDefaultListenerProbe() *DefaultListenerProbe {
	return &DefaultListenerProbe{}
}

// IsAddressPortOwnedByPID checks whether socket inode on address:port belongs to pid
func (p *DefaultListenerProbe) IsAddressPortOwnedByPID(pid int, address string, port int) (bool, error) {
	if pid <= 0 || port <= 0 {
		return false, nil
	}

	// 1. Extract all socket inodes owned by pid from /proc/<pid>/fd
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return false, err // fail-closed if /proc is unreadable
	}

	ownedInodes := make(map[uint64]bool)
	for _, e := range entries {
		target, err := os.Readlink(filepath.Join(fdDir, e.Name()))
		if err != nil {
			continue
		}
		if strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
			inodeStr := target[len("socket:[") : len(target)-1]
			if inode, err := strconv.ParseUint(inodeStr, 10, 64); err == nil {
				ownedInodes[inode] = true
			}
		}
	}

	if len(ownedInodes) == 0 {
		return false, nil
	}

	// 2. Scan /proc/net/tcp and /proc/net/tcp6 for listening sockets matching port and address
	matched, err := matchSocketInodeInProcNet(ownedInodes, address, port)
	if err != nil {
		return false, err
	}
	return matched, nil
}

// GetAddressPortOwnerPID searches /proc to find which PID owns the listening socket on address:port
func (p *DefaultListenerProbe) GetAddressPortOwnerPID(address string, port int) (int, error) {
	procEntries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}

	for _, pe := range procEntries {
		if !pe.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(pe.Name())
		if err != nil || pid <= 0 {
			continue
		}

		if owned, _ := p.IsAddressPortOwnedByPID(pid, address, port); owned {
			return pid, nil
		}
	}
	return 0, nil
}

func matchSocketInodeInProcNet(ownedInodes map[uint64]bool, address string, port int) (bool, error) {
	for _, procFile := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		f, err := os.Open(procFile)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		isHeader := true
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if isHeader {
				isHeader = false
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 10 {
				continue
			}
			// fields[1]: local_address (HEX_IP:HEX_PORT)
			// fields[3]: st (0A is TCP_LISTEN)
			// fields[9]: inode
			st := fields[3]
			if st != "0A" {
				continue
			}

			localParts := strings.Split(fields[1], ":")
			if len(localParts) != 2 {
				continue
			}
			hexPort, err := strconv.ParseInt(localParts[1], 16, 32)
			if err != nil || int(hexPort) != port {
				continue
			}

			// Validate address if specified (0.0.0.0/wildcard matches any local bind)
			if address != "" && address != "0.0.0.0" && address != "::" {
				if !matchesHexIP(localParts[0], address) {
					continue
				}
			}

			inode, err := strconv.ParseUint(fields[9], 10, 64)
			if err == nil && ownedInodes[inode] {
				f.Close()
				return true, nil
			}
		}
		f.Close()
	}
	return false, nil
}

func matchesHexIP(hexIP string, targetIP string) bool {
	// targetIP parsing
	ip := net.ParseIP(targetIP)
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 != nil && len(hexIP) == 8 {
		// Little-endian IPv4 in /proc/net/tcp
		val, err := strconv.ParseUint(hexIP, 16, 32)
		if err != nil {
			return false
		}
		b0 := byte(val & 0xFF)
		b1 := byte((val >> 8) & 0xFF)
		b2 := byte((val >> 16) & 0xFF)
		b3 := byte((val >> 24) & 0xFF)
		return b0 == ip4[0] && b1 == ip4[1] && b2 == ip4[2] && b3 == ip4[3]
	}
	return true
}

// FakeListenerProbe is an in-memory stub for testing
type FakeListenerProbe struct {
	Owners map[string]int // "address:port" -> pid
}

func NewFakeListenerProbe() *FakeListenerProbe {
	return &FakeListenerProbe{
		Owners: make(map[string]int),
	}
}

func (f *FakeListenerProbe) SetOwner(address string, port int, pid int) {
	key := fmt.Sprintf("%s:%d", address, port)
	f.Owners[key] = pid
}

func (f *FakeListenerProbe) Clear(address string, port int) {
	key := fmt.Sprintf("%s:%d", address, port)
	delete(f.Owners, key)
}

func (f *FakeListenerProbe) IsAddressPortOwnedByPID(pid int, address string, port int) (bool, error) {
	key := fmt.Sprintf("%s:%d", address, port)
	owner, ok := f.Owners[key]
	if !ok {
		// Fallback to wildcard check if binding to 0.0.0.0
		wildkey := fmt.Sprintf("0.0.0.0:%d", port)
		owner, ok = f.Owners[wildkey]
	}
	return ok && owner == pid, nil
}

func (f *FakeListenerProbe) GetAddressPortOwnerPID(address string, port int) (int, error) {
	key := fmt.Sprintf("%s:%d", address, port)
	if owner, ok := f.Owners[key]; ok {
		return owner, nil
	}
	wildkey := fmt.Sprintf("0.0.0.0:%d", port)
	if owner, ok := f.Owners[wildkey]; ok {
		return owner, nil
	}
	return 0, nil
}
