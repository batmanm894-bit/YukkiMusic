/*
 * ● YukkiMusic
 * ○ A high-performance engine for streaming music in Telegram voicechats.
 *
 * Copyright (C) 2026 TheTeamVivek
 *
 * This program is free software: you can redistribute it and/or modify it under the
 * terms of the GNU General Public License as published by the Free Software Foundation,
 * either version 3 of the License, or (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful, but WITHOUT ANY
 * WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A
 * PARTICULAR PURPOSE. See the GNU General Public License for more details.
 *
 * Repository: https://github.com/TheTeamVivek/YukkiMusic
 */

package modules

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Laky-64/gologging"
)

// diskCleanupInterval is how often we sweep for stale files.
const diskCleanupInterval = 15 * time.Minute

// diskCleanupMaxAge is how old a file must be before it's eligible for
// deletion. Track downloads are already cleaned up promptly elsewhere
// (see internal/core/room_file.go) as soon as they're no longer queued
// anywhere, so this is mainly a safety net for those plus anything that
// never gets cleaned up on its own - most importantly per-track
// thumbnails in cache/, which are cached forever by design (see
// downloadThumbnail in internal/platforms/registry.go) and would
// otherwise accumulate without bound for as long as the process stays up.
const diskCleanupMaxAge = 15 * time.Minute

// diskCleanupDirs are swept for stale files. Only files matching a known
// prefix are touched (see isCleanableFile) - anything else (e.g. the
// gogram session cache.db files) is left alone.
var diskCleanupDirs = []string{"cache", "downloads"}

// StartDiskCleanup periodically removes old cached/downloaded files so
// long-running processes don't slowly fill up disk (which, left
// unchecked, can eventually get the process killed by the host for
// running out of storage).
func StartDiskCleanup() {
	ticker := time.NewTicker(diskCleanupInterval)
	defer ticker.Stop()

	sweepStaleFiles()
	for range ticker.C {
		sweepStaleFiles()
	}
}

func sweepStaleFiles() {
	cutoff := time.Now().Add(-diskCleanupMaxAge)
	removed := 0

	for _, dir := range diskCleanupDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() || !isCleanableFile(entry.Name()) {
				continue
			}

			path := filepath.Join(dir, entry.Name())

			info, err := entry.Info()
			if err != nil || info.ModTime().After(cutoff) {
				continue
			}

			if err := os.Remove(path); err == nil {
				removed++
				gologging.DebugF("Disk cleanup: removed stale file %s", path)
			}
		}
	}

	if removed > 0 {
		gologging.InfoF("Disk cleanup: removed %d stale file(s)", removed)
	}
}

// isCleanableFile restricts the sweep to files we know are safe to
// delete and re-fetch on demand (track audio/video, thumbnails). It
// deliberately excludes anything else that might live in cache/ or
// downloads/ - e.g. the gogram session cache (cache.db, cache_*.db) or
// cookie files, which must never be swept by age.
func isCleanableFile(name string) bool {
	for _, prefix := range []string{"audio_", "video_", "thumb_"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

