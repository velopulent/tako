//go:build !debian && !ubuntu && !fedora && !rhel && !rocky && !almalinux && !archlinux && !opensuse

package distro

// This deliberately references an undefined symbol so an untagged production
// build fails with a useful, stable identifier.
var _ = distro_build_tag_required
