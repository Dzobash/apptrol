#!/usr/bin/env bash
# Puts a test fixture in place for the manual test cases (ADR 0031):
#
#   docs/testing/apply-fixture.sh <name>    e.g. two-apps; see docs/testing/fixtures/
#   docs/testing/apply-fixture.sh --restore puts your own configuration back
#
# The first time, your config.toml and state.json are moved aside (*.before-test); they
# are never overwritten by a later run. Each fixture starts with an empty saved state.
# Two quiet test tones (mpv, as apptrol-test-a and apptrol-test-b) and a virtual
# microphone (apptrol-test-mic) are started if they are not running. --restore stops
# them, removes the fixture's test log file and puts both of your files back.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
conf_dir=${XDG_CONFIG_HOME:-$HOME/.config}/apptrol
state_dir=${XDG_STATE_HOME:-$HOME/.local/state}/apptrol
run_dir=${XDG_RUNTIME_DIR:?XDG_RUNTIME_DIR is not set; run this in your desktop session}/apptrol-test
config=$conf_dir/config.toml
state=$state_dir/state.json
backup_suffix=.before-test
none_marker=.none # the backup of a file that did not exist

say() { printf '==> %s\n' "$*"; }
die() { printf 'apply-fixture: %s\n' "$*" >&2; exit 1; }

usage() {
	echo "usage: $0 <fixture> | --restore"
	echo "fixtures:"
	for f in "$here"/fixtures/*.toml; do echo "  $(basename "$f" .toml)"; done
	exit 2
}

# backup moves a file aside, once; a missing file gets a marker instead.
backup() {
	local file=$1
	if [[ -e $file$backup_suffix || -e $file$backup_suffix$none_marker ]]; then
		return # already saved by an earlier run: never overwrite it
	fi
	if [[ -e $file ]]; then
		mv -- "$file" "$file$backup_suffix"
		say "saved your $(basename "$file") as $(basename "$file$backup_suffix")"
	else
		mkdir -p -- "$(dirname "$file")"
		touch -- "$file$backup_suffix$none_marker"
	fi
}

# restore puts a file back from its backup, or removes it if there was none.
restore() {
	local file=$1
	if [[ -e $file$backup_suffix ]]; then
		mv -f -- "$file$backup_suffix" "$file"
		say "put your $(basename "$file") back"
	elif [[ -e $file$backup_suffix$none_marker ]]; then
		rm -f -- "$file" "$file$backup_suffix$none_marker"
		say "removed the test $(basename "$file") (you had none)"
	fi
}

start_tone() {
	local name=$1 freq=$2 pidfile=$run_dir/$1.pid
	if [[ -e $pidfile ]] && is_our_mpv "$(cat "$pidfile")"; then
		return
	fi
	# No user config and no scripts: no media player (MPRIS) for the tone, the same on
	# every computer. A sine tone at 40 % of mpv's own volume, so no music file is needed.
	setsid mpv --no-config --load-scripts=no --no-video --really-quiet --volume=40 \
		--audio-client-name="$name" "av://lavfi:sine=frequency=$freq" >/dev/null 2>&1 &
	echo $! >"$pidfile"
	sleep 1
	is_our_mpv "$!" || die "mpv did not start for $name (is mpv installed?)"
	say "started test tone $name ($freq Hz)"
}

is_our_mpv() { [[ -n $1 && $(ps -p "$1" -o comm= 2>/dev/null) == mpv ]]; }

stop_tones() {
	local pidfile pid
	for pidfile in "$run_dir"/*.pid; do
		[[ -e $pidfile ]] || continue
		pid=$(cat "$pidfile")
		if is_our_mpv "$pid"; then
			kill "$pid"
			say "stopped test tone $(basename "$pidfile" .pid)"
		fi
		rm -f -- "$pidfile"
	done
}

# The virtual microphone is made as `make test-audio` makes its inputs: a null sink,
# and a source that remaps its monitor (monitors are never matched as inputs).
start_mic() {
	local modules=$run_dir/modules
	[[ -s $modules ]] && return
	{
		pactl load-module module-null-sink sink_name=apptrol-test-mic-sink
		pactl load-module module-remap-source master=apptrol-test-mic-sink.monitor source_name=apptrol-test-mic
	} >"$modules"
	say "created the virtual microphone apptrol-test-mic"
}

stop_mic() {
	local modules=$run_dir/modules id
	[[ -s $modules ]] || return 0
	# Unload in reverse order: the source before the sink it remaps.
	for id in $(tac "$modules"); do pactl unload-module "$id" || true; done
	rm -f -- "$modules"
	say "removed the virtual microphone"
}

[[ $# -eq 1 ]] || usage

if [[ $1 == --restore ]]; then
	[[ -e $config$backup_suffix || -e $config$backup_suffix$none_marker ]] ||
		die "nothing to restore: no fixture was applied"
	systemctl --user stop apptrol
	stop_tones
	stop_mic
	restore "$config"
	restore "$state"
	rm -f -- "$state_dir"/apptrol-test.log* # the log file of the full-layout fixture
	systemctl --user start apptrol
	say "done: Apptrol runs with your own configuration again"
	exit 0
fi

fixture=$here/fixtures/$1.toml
[[ $1 != -* && -e $fixture ]] || usage
for tool in mpv pactl systemctl; do
	command -v "$tool" >/dev/null || die "$tool is needed but not installed"
done
mkdir -p -- "$run_dir"

systemctl --user stop apptrol # it saves its state when it stops; that goes aside too
backup "$config"
backup "$state"
cp -- "$fixture" "$config"
rm -f -- "$state"
say "applied fixture $1, with an empty saved state"

start_mic
start_tone apptrol-test-a 440
start_tone apptrol-test-b 660

systemctl --user start apptrol
say "done: Apptrol runs with fixture $1; --restore puts your configuration back"
