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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"

	"github.com/Laky-64/gologging"
	"github.com/amarnathcjd/gogram/telegram"

	"main/internal/config"
	state "main/internal/core/models"
	"main/internal/utils"
)

const PlatformShrutiAPI state.PlatformName = "ShrutiAPI"

// shrutiAPIErrorResponse covers the JSON shape ShrutiAPI sends back on
// failure (e.g. invalid key, rate limit). On success it does NOT return
// JSON at all - it streams the raw audio/video bytes directly as the
// response body - so this is only used to extract a readable message when
// something goes wrong.
type shrutiAPIErrorResponse struct {
	Message string `json:"message"`
	Error   string `json:"error"`
}

func (r shrutiAPIErrorResponse) text() string {
	if r.Message != "" {
		return r.Message
	}
	return r.Error
}

type ShrutiAPIPlatform struct {
	name state.PlatformName
}

func init() {
	// Tried right after FallenApi (80) and before YT-DLP (60).
	Register(75, &ShrutiAPIPlatform{
		name: PlatformShrutiAPI,
	})
}

func (s *ShrutiAPIPlatform) Name() state.PlatformName {
	return s.name
}

func (s *ShrutiAPIPlatform) CanGetTracks(query string) bool {
	return false
}

func (s *ShrutiAPIPlatform) GetTracks(
	_ string,
	_ bool,
) ([]*state.Track, error) {
	return nil, errors.New("shrutiapi is a download-only platform")
}

func (s *ShrutiAPIPlatform) CanDownload(source state.PlatformName) bool {
	if len(config.ShrutiAPIURLs) == 0 || len(config.ShrutiAPIKeys) == 0 {
		return false
	}
	return source == PlatformYouTube
}

func (s *ShrutiAPIPlatform) Download(
	ctx context.Context,
	track *state.Track,
	statusMsg *telegram.NewMessage,
) (string, error) {
	if f := findFile(track); f != "" {
		gologging.Debug("ShrutiAPI: Download -> Cached File -> " + f)
		return f, nil
	}
	return s.downloadToDisk(ctx, track, statusMsg)
}

// downloadToDisk fetches the track from ShrutiAPI and writes it straight
// to disk. ShrutiAPI serves the media file directly in the response body
// (confirmed from logs: a raw fragmented-mp4/m4a container), not a JSON
// object pointing to a separate URL - so there's no "instant stream" path
// like FallenApi's CDN links; every request is a full download.
func (s *ShrutiAPIPlatform) downloadToDisk(
	ctx context.Context,
	track *state.Track,
	statusMsg *telegram.NewMessage,
) (string, error) {
	var pm *telegram.ProgressManager
	if statusMsg != nil {
		pm = utils.GetProgress(statusMsg)
	}

	mediaType := "audio"
	ext := ".m4a"
	if track.Video {
		mediaType = "video"
		ext = ".mp4"
	}
	path := getPath(track, ext)

	markDownloading(downloadKey(track))
	defer unmarkDownloading(downloadKey(track))

	if err := s.fetchAndSave(ctx, track.ID, mediaType, path, pm); err != nil {
		return "", err
	}

	if !fileExists(path) {
		return "", errors.New("empty file returned by API")
	}

	return path, nil
}

// fetchAndSave picks a random key each call (so usage spreads evenly
// across all configured keys instead of always starting with the first
// one), and tries each configured base URL in order for that key (the
// announcement says the three endpoints are interchangeable). If a key is
// exhausted (daily limit, etc.) on every URL, it falls through to the next
// key. On success, the response body (raw media bytes) is written
// directly to path.
func (s *ShrutiAPIPlatform) fetchAndSave(
	ctx context.Context,
	videoID, mediaType, path string,
	pm *telegram.ProgressManager,
) error {
	var lastErr error

	for _, key := range shuffledKeys(config.ShrutiAPIKeys) {
		for _, base := range config.ShrutiAPIURLs {
			apiReqURL := fmt.Sprintf(
				"%s/download?url=%s&type=%s&api_key=%s",
				base,
				url.QueryEscape(videoID),
				mediaType,
				key,
			)

			resp, err := rc.R().SetContext(ctx).Get(apiReqURL)
			if err != nil {
				if errors.Is(err, context.Canceled) ||
					errors.Is(err, context.DeadlineExceeded) {
					return err
				}
				lastErr = sanitizeAPIError(
					fmt.Errorf("shrutiapi request to %s failed: %w", base, err),
					key,
				)
				continue
			}

			if resp.StatusCode() >= 400 {
				msg := resp.String()
				var errResp shrutiAPIErrorResponse
				if jsonErr := json.Unmarshal(resp.Bytes(), &errResp); jsonErr == nil && errResp.text() != "" {
					msg = errResp.text()
				}
				lastErr = sanitizeAPIError(fmt.Errorf(
					"shrutiapi request to %s failed with status: %d body: %s",
					base, resp.StatusCode(), msg,
				), key)
				gologging.Debug("ShrutiAPI: key/url failed, trying next -> " + lastErr.Error())
				continue
			}

			body := resp.Bytes()
			if len(body) == 0 {
				lastErr = sanitizeAPIError(fmt.Errorf(
					"shrutiapi at %s returned an empty response", base,
				), key)
				continue
			}

			if err := os.WriteFile(path, body, 0o600); err != nil {
				return fmt.Errorf("failed to write file: %w", err)
			}

			if err := verifyMediaFile(ctx, path); err != nil {
				os.Remove(path)
				lastErr = sanitizeAPIError(fmt.Errorf(
					"shrutiapi at %s returned a corrupt/truncated file: %w", base, err,
				), key)
				gologging.Debug("ShrutiAPI: " + lastErr.Error())
				continue
			}

			_ = pm // reserved: wire up progress reporting here if/when needed
			return nil
		}
	}

	if lastErr == nil {
		lastErr = errors.New("shrutiapi: no keys/endpoints configured")
	}
	gologging.Error(lastErr.Error())
	return lastErr
}
