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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Laky-64/gologging"
	"github.com/amarnathcjd/gogram/telegram"

	"main/internal/config"
	"main/internal/cookies"
	state "main/internal/core/models"
)

const PlatformYtDlp state.PlatformName = "YtDlp"

type YtdlpPlatform struct {
	name state.PlatformName
}

type ytdlpInfo struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Duration    float64     `json:"duration"`
	Thumbnail   string      `json:"thumbnail"`
	URL         string      `json:"webpage_url"`
	OriginalURL string      `json:"original_url"`
	Uploader    string      `json:"uploader"`
	Description string      `json:"description"`
	IsLive      bool        `json:"is_live"`
	Extractor   string      `json:"extractor"`
	Entries     []ytdlpInfo `json:"entries"`
}

var bannedExtractors = map[string]bool{
	"alphaporno":     true,
	"beeg":           true,
	"behindkink":     true,
	"bongacams":      true,
	"cam4":           true,
	"cammodels":      true,
	"camsoda":        true,
	"chaturbate":     true,
	"drtuber":        true,
	"eporner":        true,
	"erocast":        true,
	"eroprofile":     true,
	"fourtube":       true,
	"goshgay":        true,
	"hellporno":      true,
	"iwara":          true,
	"lovehomeporn":   true,
	"manyvids":       true,
	"motherless":     true,
	"murrtube":       true,
	"nonktube":       true,
	"noodlemagazine": true,
	"nubilesporn":    true,
	"nuvid":          true,
	"oftv":           true,
	"peekvids":       true,
	"pornbox":        true,
	"pornflip":       true,
	"pornhub":        true,
	"pornotube":      true,
	"pornovoisines":  true,
	"pornoxo":        true,
	"redgifs":        true,
	"redtube":        true,
	"rule34video":    true,
	"sauceplus":      true,
	"sexu":           true,
	"slutload":       true,
	"spankbang":      true,
	"stripchat":      true,
	"sunporno":       true,
	"thisvid":        true,
	"tnaflix":        true,
	"toypics":        true,
	"txxx":           true,
	"xhamster":       true,
	"xnxx":           true,
	"xvideos":        true,
	"xxxymovies":     true,
	"youjizz":        true,
	"youporn":        true,
	"zenporn":        true,
}

// URLs that are likely handled by YouTube
var youtubePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(youtube\.com|youtu\.be|music\.youtube\.com)`),
}

func init() {
	Register(60, &YtdlpPlatform{
		name: PlatformYtDlp,
	})
}

func (y *YtdlpPlatform) Name() state.PlatformName {
	return y.name
}

// CanGetTracks checks if this is a valid URL that yt-dlp might handle
func (y *YtdlpPlatform) CanGetTracks(query string) bool {
	query = strings.TrimSpace(query)
	if _, err := sanitizeMediaURL(query); err != nil {
		return false
	}

	// Must be a URL
	parsedURL, err := url.Parse(query)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return false
	}

	host := strings.ToLower(parsedURL.Host)

	// Ignore Telegram URLs ( already handled by TeleramPlatform)
	if host == "t.me" ||
		host == "telegram.me" ||
		host == "telegram.dog" ||
		strings.HasSuffix(host, ".t.me") {
		return false
	}

	return true
}

// GetTracks extracts metadata using yt-dlp
func (y *YtdlpPlatform) GetTracks(
	query string,
	video bool,
) ([]*state.Track, error) {
	query = strings.TrimSpace(query)
	safeURL, err := sanitizeMediaURL(query)
	if err != nil {
		return nil, errUnsafeURL
	}

	gologging.InfoF("YtDlp: Extracting metadata for %s", query)

	info, err := y.extractMetadata(safeURL)
	if err != nil {
		gologging.ErrorF("YtDlp: Failed to extract metadata: %v", err)
		return nil, fmt.Errorf("failed to extract metadata: %w", err)
	}

	// Check if it's a live stream
	if info.IsLive {
		gologging.Info("YtDlp: Detected live stream, returning error")
		return nil, errors.New(
			"live streams are not supported by yt-dlp downloader",
		)
	}

	// Check for banned extractor
	if bannedExtractors[strings.ToLower(info.Extractor)] {
		gologging.InfoF("YtDlp: Blocked adult content from extractor: %s", info.Extractor)
		return nil, errors.New("adult content is not allowed")
	}

	var tracks []*state.Track

	// Handle playlists
	if len(info.Entries) > 0 {
		gologging.InfoF(
			"YtDlp: Found playlist with %d entries",
			len(info.Entries),
		)
		for _, entry := range info.Entries {
			if entry.IsLive {
				continue // Skip live entries
			}
			// Check entry extractor if present (sometimes entries have their own extractor info)
			if entry.Extractor != "" &&
				bannedExtractors[strings.ToLower(entry.Extractor)] {
				gologging.InfoF(
					"YtDlp: Skipping banned entry from extractor: %s",
					entry.Extractor,
				)
				continue
			}
			track := y.infoToTrack(&entry, video)
			tracks = append(tracks, track)
		}
	} else {
		track := y.infoToTrack(info, video)
		tracks = []*state.Track{track}
	}

	if len(tracks) > 0 {
		gologging.InfoF(
			"YtDlp: Successfully extracted %d track(s)",
			len(tracks),
		)
	}

	return tracks, nil
}

func (y *YtdlpPlatform) CanDownload(source state.PlatformName) bool {
	// YtDlp can download from itself (when it extracts info)
	// and from YouTube platform
	return source == y.name || source == PlatformYouTube
}

// --- yt-dlp concurrency limiter ---
//
// yt-dlp spawns a real OS process every time it's invoked (metadata
// search, stream-URL resolve, or a background download). That's the
// heaviest per-request cost this bot has. On a small/free host running
// many active chats at once, letting an unbounded number of these run
// simultaneously can exhaust RAM/CPU and crash the whole bot. This
// semaphore caps how many yt-dlp processes may run at the same time,
// across every chat combined (tune via MAX_CONCURRENT_YTDLP).
var (
	ytdlpSem     chan struct{}
	ytdlpSemOnce sync.Once
)

func ytdlpSemaphore() chan struct{} {
	ytdlpSemOnce.Do(func() {
		n := config.MaxConcurrentYtdlp
		if n < 1 {
			n = 1
		}
		ytdlpSem = make(chan struct{}, n)
	})
	return ytdlpSem
}

// acquireYtdlpSlot blocks until a yt-dlp process slot is free, or ctx is
// canceled. Used for requests a real user is actively waiting on - they
// wait their fair turn instead of being rejected outright.
func acquireYtdlpSlot(ctx context.Context) (release func(), err error) {
	sem := ytdlpSemaphore()
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ytdlpBusy reports whether the yt-dlp process pool is currently at
// capacity. Used by best-effort background work (prefetching) to skip
// itself entirely rather than queue up and risk delaying real play
// requests that need a slot.
func ytdlpBusy() bool {
	sem := ytdlpSemaphore()
	return len(sem) >= cap(sem)
}

func (y *YtdlpPlatform) Download(
	ctx context.Context,
	track *state.Track,
	_ *telegram.NewMessage,
) (string, error) {
	if f := findFile(track); f != "" {
		gologging.Debug("Ytdlp: Download -> Cached File -> " + f)
		return f, nil
	}

	safeURL, err := sanitizeMediaURL(track.URL)
	if err != nil {
		return "", errUnsafeURL
	}

	// Video has to be downloaded (audio+video muxed together) before it can
	// play at all, so there's no "instant" path for it.
	if track.Video {
		return y.downloadToDisk(ctx, track, safeURL)
	}

	// Audio: resolve a direct, instantly-playable stream URL first (fast -
	// no download happens), and cache the real file to disk in the
	// background afterwards. This mirrors FallenApi's instant-play /
	// background-cache pattern so both platforms behave the same way to
	// the caller, and so racing the two of them is meaningful (whichever
	// resolves a URL first wins, instead of whichever downloads first).
	gologging.InfoF("YtDlp: Resolving stream URL for %s", track.Title)
	streamURL, err := y.getStreamURL(ctx, safeURL)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		gologging.Debug(
			"YtDlp: instant stream URL failed, falling back to direct download: " + err.Error(),
		)
		return y.downloadToDisk(ctx, track, safeURL)
	}

	// Cache the real file to disk in the background, using its own
	// independent context so it survives even if ctx (the caller's/race
	// context) is later canceled.
	go y.cacheInBackground(track, safeURL)

	return streamURL, nil
}

// getStreamURL asks yt-dlp for a direct, playable media URL without
// downloading anything (-g). This is what makes yt-dlp fast enough to
// meaningfully race against FallenApi.
func (y *YtdlpPlatform) getStreamURL(
	ctx context.Context,
	safeURL string,
) (string, error) {
	args := []string{
		"--no-playlist",
		"--no-warnings",
		"--no-check-certificate",
		"-f", "ba[abr>=180][abr<=360]/ba",
		"-g",
	}

	if y.isYouTubeURL(safeURL) {
		if cookieFile, err := cookies.GetRandomCookieFile(); err == nil &&
			cookieFile != "" {
			args = append(args, "--cookies", cookieFile)
		}
	}

	args = append(args, "--", safeURL)

	release, err := acquireYtdlpSlot(ctx)
	if err != nil {
		return "", err
	}
	defer release()

	gctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(gctx, "yt-dlp", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf(
			"yt-dlp -g failed: %w (%s)", err, strings.TrimSpace(stderr.String()),
		)
	}

	fields := strings.Fields(strings.TrimSpace(stdout.String()))
	if len(fields) == 0 {
		return "", errors.New("yt-dlp returned no stream URL")
	}

	// The format selector above always resolves to a single audio stream,
	// so there's exactly one URL to use.
	return fields[0], nil
}

// cacheInBackground downloads the track to disk after playback has already
// started streaming from the resolved URL (waiting backgroundCacheDelay,
// shared with FallenApi and defined in fallenapi.go, first), so subsequent
// plays hit the local cache instead of re-resolving/re-streaming.
func (y *YtdlpPlatform) cacheInBackground(track *state.Track, safeURL string) {
	time.Sleep(backgroundCacheDelay)

	bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if _, err := y.downloadToDisk(bgCtx, track, safeURL); err != nil {
		gologging.Debug("YtDlp: background cache failed -> " + err.Error())
		return
	}
	gologging.Debug("YtDlp: background cache complete for track " + track.ID)
}

// downloadToDisk runs the full yt-dlp download (and, for audio, extraction)
// to disk. Used directly for video, and used for audio both as the
// background-cache step and as a fallback if instant URL resolution fails.
func (y *YtdlpPlatform) downloadToDisk(
	ctx context.Context,
	track *state.Track,
	safeURL string,
) (string, error) {
	if f := findFile(track); f != "" {
		return f, nil
	}

	gologging.InfoF("YtDlp: Downloading %s", track.Title)

	args := []string{
		"--no-playlist",
		"--no-part",
		"--geo-bypass",
		"--no-warnings",
		"--ignore-errors",
		"--no-check-certificate",
		"-q",
		"-o", getPath(track, ".%(ext)s"),
	}

	// Format selection
	if track.Video {
		args = append(
			args,
			"-f",
			"(b[height>=360][height<=1080]/bv*[height>=360][height<=1080]/bv*)+(ba[abr>=180][abr<=360]/ba)/b",
		)
	} else {
		args = append(args,
			"-f", "ba[abr>=180][abr<=360]/ba",
			"-x",
			"--concurrent-fragments", "4",
		)
	}

	// Cookies (YouTube only)
	if y.isYouTubeURL(track.URL) {
		if cookieFile, err := cookies.GetRandomCookieFile(); err == nil &&
			cookieFile != "" {
			args = append(args, "--cookies", cookieFile)
		}
	}

	args = append(args, "--", safeURL)

	release, err := acquireYtdlpSlot(ctx)
	if err != nil {
		return "", err
	}
	defer release()

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errStr := strings.TrimSpace(stderr.String())
		outStr := strings.TrimSpace(stdout.String())

		gologging.ErrorF(
			"YtDlp: Download failed for %s: %v\nSTDOUT:\n%s\nSTDERR:\n%s",
			track.URL, err, outStr, errStr,
		)
		findAndRemove(track)

		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		return "", fmt.Errorf("yt-dlp error: %w", err)
	}

	path := findFile(track)
	if path == "" {
		return "", errors.New("yt-dlp did not return output file path")
	}

	gologging.InfoF("YtDlp: Successfully downloaded %s", path)
	return path, nil
}

// extractMetadata uses yt-dlp to extract video/audio metadata
func (y *YtdlpPlatform) extractMetadata(urlStr string) (*ytdlpInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	safeURL, err := sanitizeMediaURL(urlStr)
	if err != nil {
		return nil, errUnsafeURL
	}

	args := []string{
		"-j",
		"--flat-playlist",
		"--no-warnings",
		"--no-check-certificate",
	}

	// Add cookies only for YouTube
	if y.isYouTubeURL(urlStr) {
		cookieFile, err := cookies.GetRandomCookieFile()
		if err == nil && cookieFile != "" {
			args = append(args, "--cookies", cookieFile)
		}
	}

	args = append(args, "--", safeURL)

	release, err := acquireYtdlpSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errStr := stderr.String()
		gologging.ErrorF(
			"YtDlp: Metadata extraction failed: %v\n%s",
			err,
			errStr,
		)
		return nil, fmt.Errorf("metadata extraction failed: %w", err)
	}

	output := stdout.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Handle playlists (multiple JSON objects)
	if len(lines) > 1 {
		var info ytdlpInfo
		info.Entries = make([]ytdlpInfo, 0, len(lines))

		for _, line := range lines {
			var entry ytdlpInfo
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				gologging.ErrorF("YtDlp: Failed to parse entry JSON: %v", err)
				continue
			}
			info.Entries = append(info.Entries, entry)
		}

		if len(info.Entries) == 0 {
			return nil, errors.New("no valid entries found in playlist")
		}

		return &info, nil
	}

	// Single video/audio
	var info ytdlpInfo
	if err := json.Unmarshal([]byte(output), &info); err != nil {
		gologging.ErrorF("YtDlp: Failed to parse JSON: %v", err)
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &info, nil
}

// infoToTrack converts yt-dlp info to Track
func (y *YtdlpPlatform) infoToTrack(
	info *ytdlpInfo,
	video bool,
) *state.Track {
	duration := int(info.Duration)

	// Use original_url if available, otherwise webpage_url
	trackURL := info.URL
	if info.OriginalURL != "" {
		trackURL = info.OriginalURL
	}

	return &state.Track{
		ID:       info.ID,
		Title:    info.Title,
		Duration: duration,
		Artwork:  info.Thumbnail,
		URL:      trackURL,
		Source:   PlatformYtDlp,
		Video:    video,
	}
}

// isYouTubeURL checks if the URL is from YouTube
func (y *YtdlpPlatform) isYouTubeURL(urlStr string) bool {
	for _, pattern := range youtubePatterns {
		if pattern.MatchString(urlStr) {
			return true
		}
	}
	return false
}
