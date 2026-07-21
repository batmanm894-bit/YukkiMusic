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

package platforms

import (
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Laky-64/gologging"
	"github.com/amarnathcjd/gogram/telegram"

	state "main/internal/core/models"
)

// shuffledKeys returns a copy of keys in random order, so repeated calls
// (e.g. one per song played) spread usage evenly across all configured API
// keys instead of always hitting the first one first and only reaching the
// others once it's exhausted for the day.
func shuffledKeys(keys []string) []string {
	shuffled := make([]string, len(keys))
	copy(shuffled, keys)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	return shuffled
}

// inProgressDownloads tracks destination file paths that are currently
// being written to by an active download. Multiple platforms can share the
// same destination path for a given track ID (e.g. FallenApi and YtDlp both
// write to downloads/audio_<id>.mp3), and some downloaders (gogram's
// chunked writer, yt-dlp with --no-part) create/grow the file on disk
// *before* it's complete. Without this tracker, findFile() would see that
// partially-written file, assume it's a valid finished cache, and hand it
// off to be played/uploaded while another goroutine is still writing to it
// (or has just had its download canceled mid-write) - resulting in a
// corrupt or missing file at playback time.
var (
	inProgressDownloads   = make(map[string]bool)
	inProgressDownloadsMu sync.Mutex
)

// markDownloading records that a download for this key (see
// downloadKey) is in progress. Call unmarkDownloading (typically via
// defer) once the write finishes, whether it succeeded or failed.
func markDownloading(key string) {
	inProgressDownloadsMu.Lock()
	inProgressDownloads[key] = true
	inProgressDownloadsMu.Unlock()
}

func unmarkDownloading(key string) {
	inProgressDownloadsMu.Lock()
	delete(inProgressDownloads, key)
	inProgressDownloadsMu.Unlock()
}

func isDownloading(key string) bool {
	inProgressDownloadsMu.Lock()
	defer inProgressDownloadsMu.Unlock()
	return inProgressDownloads[key]
}

// downloadKey returns the tracker key for a track: its media type and ID,
// without an extension. yt-dlp's output template leaves the final
// extension unknown until the download completes, so the key can't be an
// exact file path - it only needs to identify which track is in flight.
func downloadKey(track *state.Track) string {
	t := "audio"
	if track.Video {
		t = "video"
	}
	return t + "_" + track.ID
}

func getPath(track *state.Track, ext string) string {
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}

	mediaType := "audio"
	if track.Video {
		mediaType = "video"
	}

	filename := mediaType + "_" + track.ID + ext

	return filepath.Join("downloads", filename)
}

func fileExists(path string) bool {
	i, err := os.Stat(path)
	if err != nil {
		gologging.ErrorF("os.Stat: %v", err)
		return false
	}

	return i.Size() > 0
}

func findFile(track *state.Track) string {
	if isDownloading(downloadKey(track)) {
		return ""
	}

	t := "audio"
	if track.Video {
		t = "video"
	}

	files, err := filepath.Glob(filepath.Join("downloads", t+"_"+track.ID+"*"))
	if err != nil {
		gologging.ErrorF("filepath.Glob: %v", err)
		return ""
	}

	for _, f := range files {
		if i, err := os.Stat(f); err == nil && i.Size() > 0 {
			return f
		}
	}

	return ""
}

func findAndRemove(track *state.Track) {
	t := "audio"
	if track.Video {
		t = "video"
	}

	files, err := filepath.Glob(filepath.Join("downloads", t+"_"+track.ID+"*"))
	if err != nil {
		return
	}

	for _, f := range files {
		os.Remove(f)
	}
}

func sanitizeAPIError(err error, apiKey string) error {
	if err == nil || apiKey == "" {
		return err
	}
	masked := strings.ReplaceAll(err.Error(), apiKey, "***REDACTED***")
	return errors.New(masked)
}

func playableMedia(m *telegram.NewMessage) (bool, bool) {
	if m == nil {
		return false, false
	}

	check := func(msg *telegram.NewMessage) (bool, bool) {
		switch {
		case msg.Audio() != nil, msg.Voice() != nil:
			return false, true

		case msg.Video() != nil:
			return true, false

		case msg.Document() != nil:
			mimeType := strings.ToLower(msg.Document().MimeType)

			if mimeType == "" {
				return false, false
			}

			switch {
			case strings.HasPrefix(mimeType, "audio/"):
				return false, true
			case strings.HasPrefix(mimeType, "video/"):
				return true, false
			}
		}

		return false, false
	}

	if m.IsReply() {
		rmsg, err := m.GetReplyMessage()
		if err != nil {
			return false, false
		}
		return check(rmsg)
	}

	return check(m)
}
