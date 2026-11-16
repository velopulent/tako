package platform

import "strings"

// CompareRPMVersions compares epoch:version-release using RPM's segment rules,
// including tilde prereleases and caret snapshots. Missing epochs mean zero;
// epochs must not be discarded when matching a security fix.
func CompareRPMVersions(a, b string) int {
	ae, av, ar := splitRPMVersion(a)
	be, bv, br := splitRPMVersion(b)
	if n := compareRPMSegment(ae, be); n != 0 {
		return n
	}
	if n := compareRPMSegment(av, bv); n != 0 {
		return n
	}
	return compareRPMSegment(ar, br)
}

func splitRPMVersion(value string) (epoch, version, release string) {
	epoch, version = "0", value
	if e, rest, ok := strings.Cut(value, ":"); ok {
		epoch, version = e, rest
	}
	if index := strings.LastIndexByte(version, '-'); index >= 0 {
		version, release = version[:index], version[index+1:]
	}
	return
}

func rpmDigit(c byte) bool { return c >= '0' && c <= '9' }
func rpmAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func compareRPMSegment(a, b string) int {
	for a != "" || b != "" {
		for len(a) > 0 && !rpmDigit(a[0]) && !rpmAlpha(a[0]) && a[0] != '~' && a[0] != '^' {
			a = a[1:]
		}
		for len(b) > 0 && !rpmDigit(b[0]) && !rpmAlpha(b[0]) && b[0] != '~' && b[0] != '^' {
			b = b[1:]
		}
		at, bt := strings.HasPrefix(a, "~"), strings.HasPrefix(b, "~")
		if at || bt {
			if !at {
				return 1
			}
			if !bt {
				return -1
			}
			a, b = a[1:], b[1:]
			continue
		}
		ac, bc := strings.HasPrefix(a, "^"), strings.HasPrefix(b, "^")
		if ac || bc {
			if a == "" {
				return -1
			}
			if b == "" {
				return 1
			}
			if !ac {
				return 1
			}
			if !bc {
				return -1
			}
			a, b = a[1:], b[1:]
			continue
		}
		if a == "" || b == "" {
			break
		}
		numeric := rpmDigit(a[0])
		match := rpmAlpha
		if numeric {
			match = rpmDigit
		}
		i, j := 0, 0
		for i < len(a) && match(a[i]) {
			i++
		}
		for j < len(b) && match(b[j]) {
			j++
		}
		if j == 0 {
			if numeric {
				return 1
			}
			return -1
		}
		x, y := a[:i], b[:j]
		if numeric {
			x, y = strings.TrimLeft(x, "0"), strings.TrimLeft(y, "0")
			if len(x) > len(y) {
				return 1
			}
			if len(x) < len(y) {
				return -1
			}
		}
		if n := strings.Compare(x, y); n != 0 {
			return n
		}
		a, b = a[i:], b[j:]
	}
	if a != "" {
		return 1
	}
	if b != "" {
		return -1
	}
	return 0
}
