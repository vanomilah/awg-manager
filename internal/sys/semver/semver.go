// Package semver provides minimal semantic-version comparison for
// dotted-numeric version strings like "2.3.10" or "1.4".
//
// Build metadata (+r70 from CI) and pre-release suffixes on a component
// (-rc, -beta, …) are stripped before comparison so "2.11.2+r70"
// equals repo "2.11.2". Used by the updater and kernel-module manager.
package semver

import (
	"strconv"
	"strings"
)

// Base returns the release version without build metadata (+suffix).
// CHANGELOG.md and entware Packages use this form (e.g. "2.11.2").
func Base(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	return v
}

// Compare compares two version strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b. Missing components are
// treated as 0, so "1.2" == "1.2.0". Build tags (+rN) and pre-release
// suffixes on a component (-rc, -beta, …) are ignored.
func Compare(a, b string) int {
	return compareParts(strings.Split(Base(a), "."), strings.Split(Base(b), "."))
}

func compareParts(partsA, partsB []string) int {
	maxLen := len(partsA)
	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}
	for i := 0; i < maxLen; i++ {
		var partA, partB string
		if i < len(partsA) {
			partA = partsA[i]
		}
		if i < len(partsB) {
			partB = partsB[i]
		}
		numA := parseComponent(partA)
		numB := parseComponent(partB)
		if numA < numB {
			return -1
		}
		if numA > numB {
			return 1
		}
	}
	return 0
}

// parseComponent returns the numeric patch/segment value, ignoring
// pre-release suffixes (-rc1, -beta, …) on that component.
func parseComponent(part string) int {
	part = strings.TrimSpace(part)
	if part == "" {
		return 0
	}
	if i := strings.IndexByte(part, '-'); i >= 0 {
		part = part[:i]
	}
	if part == "" {
		return 0
	}
	n, err := strconv.Atoi(part)
	if err != nil {
		return 0
	}
	return n
}
