#!/bin/sh
set -eu

scripts_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
mock_bin="$work/bin"
runtime_dir="$work/systemd"
state_dir="$work/state"
log="$work/calls.log"
mkdir -p "$mock_bin" "$runtime_dir" "$state_dir"

cat >"$mock_bin/mock" <<'EOF'
#!/bin/sh
set -eu
name=$(basename "$0")
printf '%s %s\n' "$name" "$*" >>"$MOCK_LOG"

has_unit() {
    list=" $1 "
    case "$list" in *" $2 "*) return 0 ;; *) return 1 ;; esac
}

case "$name:$1" in
    systemctl:is-active)
        unit=${3:-}
        if has_unit "${MOCK_ACTIVE_UNITS:-}" "$unit"; then
            exit 0
        fi
        exit 1
        ;;
    systemctl:is-enabled)
        unit=${2:-}
        if has_unit "${MOCK_ADMIN_MASKED_UNITS:-}" "$unit" || [ -f "$MOCK_STATE_DIR/legacy-$unit" ]; then
            echo masked
            exit 1
        fi
        if has_unit "${MOCK_DISABLED_UNITS:-}" "$unit"; then
            echo disabled
            exit 1
        fi
        echo enabled
        ;;
    systemctl:is-failed)
        unit=${3:-}
        if has_unit "${MOCK_FAILED_UNITS:-}" "$unit"; then
            exit 0
        fi
        exit 1
        ;;
    deb-systemd-helper:unmask)
        rm -f "$MOCK_STATE_DIR/legacy-${2:-}"
        ;;
    deb-systemd-helper:--quiet)
        unit=${3:-}
        if has_unit "${MOCK_WAS_DISABLED_UNITS:-}" "$unit"; then
            exit 1
        fi
        exit 0
        ;;
esac
exit 0
EOF
chmod +x "$mock_bin/mock"
for name in systemctl deb-systemd-helper deb-systemd-invoke systemd-sysusers systemd-tmpfiles; do
    ln -s mock "$mock_bin/$name"
done

run_script() {
    : >"$log"
    PATH="$mock_bin:$PATH" \
        MOCK_LOG="$log" \
        MOCK_STATE_DIR="$state_dir" \
        TAKO_SYSTEMD_RUNTIME_DIR="$runtime_dir" \
        MOCK_ACTIVE_UNITS="${MOCK_ACTIVE_UNITS:-}" \
        MOCK_ADMIN_MASKED_UNITS="${MOCK_ADMIN_MASKED_UNITS:-}" \
        MOCK_DISABLED_UNITS="${MOCK_DISABLED_UNITS:-}" \
        MOCK_WAS_DISABLED_UNITS="${MOCK_WAS_DISABLED_UNITS:-}" \
        MOCK_FAILED_UNITS="${MOCK_FAILED_UNITS:-}" \
        TAKO_PACKAGE_STATE_DIR="$state_dir" \
        DPKG_ROOT="${DPKG_ROOT:-}" \
        sh "$@"
}

assert_has() {
    grep -Fqx "$1" "$log" || { echo "missing call: $1" >&2; cat "$log" >&2; exit 1; }
}

assert_lacks() {
    if grep -Fq "$1" "$log"; then
        echo "unexpected call containing: $1" >&2
        cat "$log" >&2
        exit 1
    fi
}

assert_before() {
    first=$(grep -nF "$1" "$log" | head -1 | cut -d: -f1)
    second=$(grep -nF "$2" "$log" | head -1 | cut -d: -f1)
    [ -n "$first" ] && [ -n "$second" ] && [ "$first" -lt "$second" ] || {
        echo "wrong call order: $1 before $2" >&2
        cat "$log" >&2
        exit 1
    }
}

deb_postinst="$scripts_dir/deb/postinst.sh"
deb_prerm="$scripts_dir/deb/prerm.sh"
deb_postrm="$scripts_dir/deb/postrm.sh"
rpm_post="$scripts_dir/rpm/post.sh"
rpm_preun="$scripts_dir/rpm/preun.sh"
rpm_postun="$scripts_dir/rpm/postun.sh"
arch_post="$scripts_dir/arch/post.sh"
arch_preun="$scripts_dir/arch/preun.sh"

# Debian fresh install: repair old helper mask, enable through helper state,
# then start private socket before public socket.
touch "$state_dir/legacy-tako.socket" "$state_dir/legacy-tako-sessiond.socket"
run_script "$deb_postinst" configure
assert_before "deb-systemd-helper unmask tako-sessiond.socket" "deb-systemd-helper --quiet was-enabled tako-sessiond.socket"
assert_has "deb-systemd-helper enable tako-sessiond.socket"
assert_before "deb-systemd-invoke start tako-sessiond.socket" "deb-systemd-invoke start tako.socket"
assert_lacks " mask "

touch "$state_dir/legacy-tako.socket" "$state_dir/legacy-tako-sessiond.socket"
run_script "$deb_postinst" configure 0.9.0
assert_has "deb-systemd-invoke start tako-sessiond.socket"
assert_has "deb-systemd-invoke start tako.socket"

# Disabled helper state is updated, not re-enabled.
MOCK_WAS_DISABLED_UNITS="tako.socket" run_script "$deb_postinst" configure
assert_has "deb-systemd-helper update-state tako.socket"
assert_lacks "deb-systemd-helper enable tako.socket"

# Upgrade: stop live services first, clear failures, restart dependency socket
# first, and restore only services that had been active.
MOCK_ACTIVE_UNITS="tako-sessiond.socket tako.socket tako-sessiond.service tako.service" \
    run_script "$deb_postinst" configure 1.0.0
assert_before "deb-systemd-invoke stop tako.service" "deb-systemd-invoke restart tako-sessiond.socket"
assert_before "systemctl reset-failed" "deb-systemd-invoke restart tako-sessiond.socket"
assert_before "deb-systemd-invoke restart tako-sessiond.socket" "deb-systemd-invoke restart tako.socket"
assert_has "deb-systemd-invoke start tako-sessiond.service"
assert_has "deb-systemd-invoke start tako.service"

# Stopped upgrade stays stopped; administrator mask survives helper unmask.
run_script "$deb_postinst" configure 1.0.0
assert_lacks "deb-systemd-invoke restart"
assert_lacks "deb-systemd-invoke start"
MOCK_ACTIVE_UNITS="tako-sessiond.socket tako.socket" MOCK_ADMIN_MASKED_UNITS="tako.socket" \
    run_script "$deb_postinst" configure 1.0.0
assert_has "deb-systemd-invoke restart tako-sessiond.socket"
assert_lacks "deb-systemd-invoke restart tako.socket"

# Failed sockets are recovered even though inactive; stopped healthy sockets
# remain stopped. Removal marker makes remove/reinstall start like fresh.
MOCK_FAILED_UNITS="tako.socket" run_script "$deb_postinst" configure 1.0.0
assert_has "deb-systemd-invoke restart tako.socket"
run_script "$deb_prerm" remove
assert_has "deb-systemd-invoke stop tako.service tako-sessiond.service tako.socket tako-sessiond.socket"
[ -f "$state_dir/package-removed" ] || { echo "removal marker missing" >&2; exit 1; }
run_script "$deb_postinst" configure 1.0.0
assert_has "deb-systemd-invoke start tako-sessiond.socket"
assert_has "deb-systemd-invoke start tako.socket"
[ ! -f "$state_dir/package-removed" ] || { echo "removal marker not cleared" >&2; exit 1; }

# Offline roots create users/directories in target but never contact PID 1.
DPKG_ROOT="$work/root" run_script "$deb_postinst" configure
assert_has "systemd-sysusers --root=$work/root tako.conf"
assert_has "systemd-tmpfiles --root=$work/root --create tako.conf"
assert_lacks "systemctl "
assert_lacks "deb-systemd-invoke "

# All documented Debian recovery entry points run; unrelated calls are no-ops.
for action in configure abort-upgrade abort-deconfigure abort-remove; do
    run_script "$deb_postinst" "$action" 1.0.0
    assert_has "deb-systemd-helper unmask tako-sessiond.socket"
done
run_script "$deb_postinst" triggered
[ ! -s "$log" ] || { cat "$log" >&2; exit 1; }

for action in upgrade deconfigure failed-upgrade; do
    run_script "$deb_prerm" "$action"
    assert_lacks "deb-systemd-invoke stop"
done
run_script "$deb_prerm" remove
assert_has "deb-systemd-invoke stop tako.service tako-sessiond.service tako.socket tako-sessiond.socket"
assert_lacks "mask"
run_script "$deb_postrm" remove
assert_lacks "deb-systemd-helper purge"
run_script "$deb_postrm" purge
assert_has "deb-systemd-helper purge tako-sessiond.socket tako.socket tako-sessiond.service tako.service"
[ ! -f "$state_dir/package-removed" ] || { echo "purge left removal marker" >&2; exit 1; }
assert_lacks "mask"

# RPM fresh install and upgrade use same safe ordering and preserve stopped or
# masked runtime state.
run_script "$rpm_post" 1
assert_before "systemctl start tako-sessiond.socket" "systemctl start tako.socket"
assert_has "systemctl enable tako-sessiond.socket"
assert_has "systemctl enable tako.socket"

MOCK_ACTIVE_UNITS="tako-sessiond.socket tako.socket tako-sessiond.service tako.service" run_script "$rpm_post" 2
assert_before "systemctl stop tako.service" "systemctl restart tako-sessiond.socket"
assert_before "systemctl reset-failed" "systemctl restart tako-sessiond.socket"
assert_before "systemctl restart tako-sessiond.socket" "systemctl restart tako.socket"
assert_lacks "systemctl enable"
assert_has "systemctl start tako-sessiond.service"
assert_has "systemctl start tako.service"

run_script "$rpm_post" 3
assert_lacks "systemctl restart"
assert_lacks "systemctl start"
assert_lacks "systemctl enable"
MOCK_ACTIVE_UNITS="tako-sessiond.socket tako.socket" MOCK_ADMIN_MASKED_UNITS="tako.socket" run_script "$rpm_post" 2
assert_has "systemctl restart tako-sessiond.socket"
assert_lacks "systemctl restart tako.socket"
MOCK_FAILED_UNITS="tako.socket" run_script "$rpm_post" 2
assert_has "systemctl restart tako.socket"

run_script "$rpm_preun" 0
assert_has "systemctl stop tako.service tako-sessiond.service tako.socket tako-sessiond.socket"
assert_has "systemctl disable tako-sessiond.socket tako.socket"
assert_lacks "mask"
for count in 1 2; do
    run_script "$rpm_preun" "$count"
    [ ! -s "$log" ] || { cat "$log" >&2; exit 1; }
done
run_script "$rpm_postun" 0
assert_has "systemctl daemon-reload"

# Arch package hooks use pacman's install/upgrade action and preserve the same
# socket ordering and administrator state as the RPM path.
run_script "$arch_post" install
assert_before "systemctl start tako-sessiond.socket" "systemctl start tako.socket"
assert_has "systemctl enable tako-sessiond.socket"
assert_has "systemctl enable tako.socket"

MOCK_ACTIVE_UNITS="tako-sessiond.socket tako.socket tako-sessiond.service tako.service" run_script "$arch_post" upgrade
assert_before "systemctl stop tako.service" "systemctl restart tako-sessiond.socket"
assert_before "systemctl reset-failed" "systemctl restart tako-sessiond.socket"
assert_before "systemctl restart tako-sessiond.socket" "systemctl restart tako.socket"
assert_has "systemctl start tako-sessiond.service"
assert_has "systemctl start tako.service"

run_script "$arch_post" upgrade
assert_lacks "systemctl restart"
assert_lacks "systemctl start"
assert_lacks "systemctl enable"
MOCK_FAILED_UNITS="tako.socket" run_script "$arch_post" upgrade
assert_has "systemctl restart tako.socket"

run_script "$arch_preun" remove
assert_has "systemctl stop tako.service tako-sessiond.service tako.socket tako-sessiond.socket"
assert_has "systemctl disable tako-sessiond.socket tako.socket"
assert_lacks "mask"

echo "Maintainer script tests passed"
