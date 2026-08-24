#!/bin/bash
# barnard-memwatch.sh
# Description: Records barnard's memory use alongside its Go heap size, and
# captures profiles before the kernel's OOM killer can take the evidence away.
#
# Start barnard with -profile, then run this alongside it. The ratio between
# the two numbers is the diagnosis:
#
#   RSS large, go_heap small -> the growth is in cgo memory (OpenAL, opus,
#                               rnnoise). The Go garbage collector cannot see
#                               it and so applies no back pressure at all.
#   RSS large, go_heap large -> the growth is Go-side, and the heap profile
#                               this script dumps names what is holding it.
#
# Usage: barnard-memwatch.sh [output-log]
#
# Copyright 2026, Storm Dragon, <storm_dragon@linux-a11y.org>
#
# This is free software; you can redistribute it and/or modify it under the
# terms of the GNU General Public License as published by the Free
# Software Foundation; either version 3, or (at your option) any later
# version.
#
# This software is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU
# General Public License for more details.

set -u

# barnard serves its profiles here when started with -profile.
profile_host="localhost:6060"
# Dump profiles once resident memory passes this many kilobytes.
dump_threshold_kb=2000000
sample_interval=30

output="${1:-$HOME/barnard-memwatch.log}"

pid="$(pgrep -n -x barnard)" || {
    echo "barnard is not running" >&2
    exit 1
}

if ! curl -s -m 2 "http://${profile_host}/debug/pprof/" > /dev/null; then
    echo "no profile server on ${profile_host}; start barnard with -profile" >&2
    exit 1
fi

echo "watching barnard (pid ${pid}), writing to ${output}"

dumped=0
while kill -0 "$pid" 2> /dev/null; do
    rss="$(awk '/^VmRSS/{print $2}' "/proc/${pid}/status" 2> /dev/null)"
    swap="$(awk '/^VmSwap/{print $2}' "/proc/${pid}/status" 2> /dev/null)"
    heap="$(curl -s -m 2 "http://${profile_host}/debug/pprof/heap?debug=1" \
        | awk '/^# HeapInuse/{print $4}')"
    goroutines="$(curl -s -m 2 "http://${profile_host}/debug/pprof/goroutine?debug=1" \
        | head -1 | grep -o '[0-9]*')"
    threads="$(ls "/proc/${pid}/task" 2> /dev/null | wc -l)"

    printf '%s rss=%skB swap=%skB go_heap=%sB goroutines=%s threads=%s\n' \
        "$(date +%T)" "${rss:-?}" "${swap:-0}" "${heap:-?}" \
        "${goroutines:-?}" "${threads}" >> "$output"

    # Capture the evidence once, while the process is still alive to ask.
    if [[ ${dumped} -eq 0 && ${rss:-0} -gt ${dump_threshold_kb} ]]; then
        dumped=1
        curl -s -m 10 -o "${output}.heap" \
            "http://${profile_host}/debug/pprof/heap"
        curl -s -m 10 -o "${output}.goroutine" \
            "http://${profile_host}/debug/pprof/goroutine?debug=2"
        cp "/proc/${pid}/smaps_rollup" "${output}.smaps" 2> /dev/null
        printf '%s *** dumped profiles at rss=%skB ***\n' \
            "$(date +%T)" "${rss}" >> "$output"
    fi

    sleep "${sample_interval}"
done

echo "barnard exited; log is in ${output}"
