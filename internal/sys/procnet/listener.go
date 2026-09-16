package procnet

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ListenerLookup holds the tri-state outcome of probing listening sockets in procfs.
type ListenerLookup struct {
	SocketFound bool
	SocketInode string
	PID         int
}

// IPToProcHex converts an IP address string to the Linux kernel /proc/net/tcp format (little-endian hex).
func IPToProcHex(addr string) string {
	ip := net.ParseIP(addr)
	if ip == nil {
		return ""
	}
	if ip4 := ip.To4(); ip4 != nil {
		return fmt.Sprintf("%02X%02X%02X%02X", ip4[3], ip4[2], ip4[1], ip4[0])
	}
	ip6 := ip.To16()
	if ip6 == nil {
		return ""
	}
	var b strings.Builder
	for i := 0; i < 16; i += 4 {
		fmt.Fprintf(&b, "%02X%02X%02X%02X", ip6[i+3], ip6[i+2], ip6[i+1], ip6[i])
	}
	return b.String()
}

// IsIPv6Target reports whether the address refers to an IPv6 destination.
func IsIPv6Target(addr string) bool {
	if addr == "" {
		return false
	}
	ip := net.ParseIP(addr)
	if ip != nil {
		return ip.To4() == nil
	}
	return strings.Contains(addr, ":")
}

// FindListeningProcess scans procfs socket tables for an active listening socket matching addr:port,
// and if found, resolves the owning PID from /proc/<pid>/fd symlinks.
// It strictly fails closed if required socket tables are missing, unreadable, or directories.
func FindListeningProcess(procDir string, addr string, port int) (ListenerLookup, error) {
	if procDir == "" {
		procDir = "/proc"
	}
	portHex := fmt.Sprintf("%04X", port)
	targetAddrHex := strings.ToUpper(IPToProcHex(addr))

	var tablePaths []string
	if IsIPv6Target(addr) {
		// IPv6 target: primary and mandatory table is net/tcp6
		tcp6Path := filepath.Join(procDir, "net", "tcp6")
		info, err := os.Stat(tcp6Path)
		if err != nil {
			return ListenerLookup{}, fmt.Errorf("ipv6 socket table %s is unavailable: %w", tcp6Path, err)
		}
		if info.IsDir() {
			return ListenerLookup{}, fmt.Errorf("ipv6 socket table %s is a directory", tcp6Path)
		}
		tablePaths = append(tablePaths, tcp6Path)
	} else {
		// IPv4 target: primary and mandatory table is net/tcp
		tcpPath := filepath.Join(procDir, "net", "tcp")
		info, err := os.Stat(tcpPath)
		if err != nil {
			return ListenerLookup{}, fmt.Errorf("ipv4 socket table %s is unavailable: %w", tcpPath, err)
		}
		if info.IsDir() {
			return ListenerLookup{}, fmt.Errorf("ipv4 socket table %s is a directory", tcpPath)
		}
		tablePaths = append(tablePaths, tcpPath)

		// Also check net/tcp6 if present for dual-stack wildcard bindings (e.g. :::port)
		tcp6Path := filepath.Join(procDir, "net", "tcp6")
		if info6, err := os.Stat(tcp6Path); err == nil {
			if info6.IsDir() {
				return ListenerLookup{}, fmt.Errorf("ipv6 socket table %s is a directory", tcp6Path)
			}
			tablePaths = append(tablePaths, tcp6Path)
		} else if !os.IsNotExist(err) {
			return ListenerLookup{}, fmt.Errorf("failed to access ipv6 socket table %s: %w", tcp6Path, err)
		}
	}

	var inodes []string
	socketFound := false

	for _, tableFile := range tablePaths {
		data, err := os.ReadFile(tableFile)
		if err != nil {
			return ListenerLookup{}, fmt.Errorf("failed to read socket table %s: %w", tableFile, err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 10 && fields[3] == "0A" {
				local := strings.ToUpper(fields[1])
				parts := strings.Split(local, ":")
				if len(parts) != 2 || parts[1] != portHex {
					continue
				}
				rowAddr := parts[0]
				isWildcardListener := strings.Trim(rowAddr, "0") == ""
				isWildcardTarget := addr == "" || addr == "0.0.0.0" || addr == "::"
				if isWildcardTarget || isWildcardListener || rowAddr == targetAddrHex {
					socketFound = true
					inode := fields[9]
					if inode != "" && inode != "0" {
						inodes = append(inodes, inode)
					}
				}
			}
		}
	}

	if !socketFound {
		return ListenerLookup{SocketFound: false, SocketInode: "", PID: 0}, nil
	}

	firstInode := ""
	if len(inodes) > 0 {
		firstInode = inodes[0]
	}

	if len(inodes) == 0 {
		return ListenerLookup{SocketFound: true, SocketInode: "", PID: 0}, nil
	}

	inodeMap := make(map[string]bool, len(inodes)*2)
	for _, in := range inodes {
		inodeMap[fmt.Sprintf("socket:[%s]", in)] = true
		inodeMap[in] = true
	}

	entries, err := os.ReadDir(procDir)
	if err != nil {
		return ListenerLookup{SocketFound: true, SocketInode: firstInode, PID: 0}, fmt.Errorf("failed to read proc directory %s: %w", procDir, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		fdDir := filepath.Join(procDir, entry.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err == nil {
				cleanLink := strings.TrimPrefix(strings.TrimSuffix(link, "]"), "socket:[")
				if inodeMap[link] || inodeMap[cleanLink] {
					return ListenerLookup{
						SocketFound: true,
						SocketInode: firstInode,
						PID:         pid,
					}, nil
				}
			}
		}
	}

	return ListenerLookup{
		SocketFound: true,
		SocketInode: firstInode,
		PID:         0,
	}, nil
}

// FindListeningPIDForAddressPort returns the PID listening on addr:port, or 0 if absent/unresolved.
func FindListeningPIDForAddressPort(procDir string, addr string, port int) int {
	lookup, err := FindListeningProcess(procDir, addr, port)
	if err != nil || !lookup.SocketFound {
		return 0
	}
	return lookup.PID
}

// FindListeningPIDForPort returns the PID listening on port on any interface, or 0 if absent/unresolved.
func FindListeningPIDForPort(port int) int {
	return FindListeningPIDForAddressPort("/proc", "", port)
}
