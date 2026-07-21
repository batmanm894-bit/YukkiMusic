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

// shrutiAPIResponse covers the field names we can reasonably expect from a
// download-link API. The exact schema wasn't published with the API
// announcement, so this checks a handful of common key names - adjust the
// json tags below if the real response uses something else.
type shrutiAPIResponse struct {
	URL         string `json:"url"`
	DownloadURL string `json:"download_url"`
	Link        string `json:"link"`
	CdnURL      string `json:"cdnurl"`
}

func (r shrutiAPIResponse) resolvedURL() string {
	switch {
	case r.URL != "":
		return r.URL
	case r.DownloadURL != "":
		return r.DownloadURL
	case r.Link != "":
		return r.Link
	case r.CdnURL != "":
		return r.CdnURL
	default:
		return ""
	}
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

	// Video has to be muxed/downloaded before it can play at all, so
	// there's no instant path for it - same restriction as FallenApi/YtDlp.
	if track.Video {
		return s.downloadToDisk(ctx, track, statusMsg)
	}

	dlURL, err := s.getDownloadURL(ctx, track.ID, "audio")
	if err != nil {
		return "", err
	}

	// Mirrors FallenApi/YtDlp: hand back the resolved URL directly so
	// ffmpeg streams from it immediately instead of waiting for a full
	// download first. No background disk-caching here on purpose - on
	// Render's free tier the downloads/ folder is wiped on every restart
	// anyway, so caching would just spend extra CPU/bandwidth on an
	// already-constrained host for a cache that rarely survives to be
	// reused.
	return dlURL, nil
}

// downloadToDisk is the old full-download path, kept only for video (which
// must be a local muxed file before ntgcalls can play it).
func (s *ShrutiAPIPlatform) downloadToDisk(
	ctx context.Context,
	track *state.Track,
	statusMsg *telegram.NewMessage,
) (string, error) {
	var pm *telegram.ProgressManager
	if statusMsg != nil {
		pm = utils.GetProgress(statusMsg)
	}

	dlURL, err := s.getDownloadURL(ctx, track.ID, "video")
	if err != nil {
		return "", err
	}

	path := getPath(track, ".mp4")

	markDownloading(downloadKey(track))
	defer unmarkDownloading(downloadKey(track))

	if err := s.downloadFromURL(ctx, dlURL, path, pm); err != nil {
		return "", err
	}

	if !fileExists(path) {
		return "", errors.New("empty file returned by API")
	}

	return path, nil
}

// getDownloadURL picks a random key each call (so usage spreads evenly
// across all configured keys instead of always starting with the first
// one), and tries each configured base URL in order for that key (the
// announcement says the three endpoints are interchangeable). If a key is
// exhausted (daily limit, etc.) on every URL, it falls through to the next
// key.
func (s *ShrutiAPIPlatform) getDownloadURL(
	ctx context.Context,
	videoID, mediaType string,
) (string, error) {
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

			var apiResp shrutiAPIResponse

			resp, err := rc.R().
				SetContext(ctx).
				SetResult(&apiResp).
				Get(apiReqURL)
			if err != nil {
				if errors.Is(err, context.Canceled) ||
					errors.Is(err, context.DeadlineExceeded) {
					return "", err
				}
				lastErr = sanitizeAPIError(
					fmt.Errorf("shrutiapi request to %s failed: %w", base, err),
					key,
				)
				continue
			}

			if resp.StatusCode() >= 400 {
				lastErr = sanitizeAPIError(fmt.Errorf(
					"shrutiapi request to %s failed with status: %d body: %s",
					base, resp.StatusCode(), resp.String(),
				), key)
				gologging.Debug("ShrutiAPI: key/url failed, trying next -> " + lastErr.Error())
				continue
			}

			if dl := apiResp.resolvedURL(); dl != "" {
				return dl, nil
			}

			lastErr = sanitizeAPIError(fmt.Errorf(
				"shrutiapi at %s returned no download url, body: %s",
				base, resp.String(),
			), key)
		}
	}

	if lastErr == nil {
		lastErr = errors.New("shrutiapi: no keys/endpoints configured")
	}
	gologging.Error(lastErr.Error())
	return "", lastErr
}

func (s *ShrutiAPIPlatform) downloadFromURL(
	ctx context.Context,
	dlURL, path string,
	pm *telegram.ProgressManager,
) error {
	resp, err := rc.R().SetContext(ctx).Get(dlURL)
	if err != nil {
		os.Remove(path)
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("http download failed: %w", err)
	}

	if resp.StatusCode() >= 400 {
		return fmt.Errorf("download failed with status: %d", resp.StatusCode())
	}

	if err := os.WriteFile(path, resp.Bytes(), 0o600); err != nil {
		os.Remove(path)
		return fmt.Errorf("failed to write file: %w", err)
	}

	_ = pm // reserved: wire up progress reporting here if/when needed

	return nil
}
