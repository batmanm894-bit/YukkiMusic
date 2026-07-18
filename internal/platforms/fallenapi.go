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
	"regexp"
	"strconv"
	"time"

	"github.com/Laky-64/gologging"
	"github.com/amarnathcjd/gogram/telegram"

	"main/internal/config"
	"main/internal/core"
	state "main/internal/core/models"
	"main/internal/utils"
)

var telegramDLRegex = regexp.MustCompile(
	`https:\/\/t\.me\/([a-zA-Z0-9_]{5,})\/(\d+)`,
)

const PlatformFallenApi state.PlatformName = "FallenApi"

type apiResponse struct {
	CdnUrl string `json:"cdnurl"`
}

type FallenApiPlatform struct {
	name state.PlatformName
}

func init() {
	Register(80, &FallenApiPlatform{
		name: PlatformFallenApi,
	})
}

func (f *FallenApiPlatform) Name() state.PlatformName {
	return f.name
}

func (f *FallenApiPlatform) CanGetTracks(query string) bool {
	return false
}

func (f *FallenApiPlatform) GetTracks(
	_ string,
	_ bool,
) ([]*state.Track, error) {
	return nil, errors.New("fallenapi is a download-only platform")
}

func (f *FallenApiPlatform) CanDownload(
	source state.PlatformName,
) bool {
	if config.FallenAPIURL == "" || config.FallenAPIKey == "" {
		return false
	}
	return source == PlatformYouTube
}

func (f *FallenApiPlatform) Download(
	ctx context.Context,
	track *state.Track,
	statusMsg *telegram.NewMessage,
) (string, error) {
	// fallen api didn't support video downloads so disable it
	track.Video = false

	if f := findFile(track); f != "" {
		gologging.Debug("FallenApi: Download -> Cached File -> " + f)
		return f, nil
	}

	var pm *telegram.ProgressManager
	if statusMsg != nil {
		pm = utils.GetProgress(statusMsg)
	}

	dlURL, err := f.getDownloadURL(ctx, track.URL)
	if err != nil {
		return "", err
	}

	path := getPath(track, ".mp3")

	// Telegram-hosted files must be downloaded via the Telegram client;
	// there's no direct HTTP stream URL for them.
	if telegramDLRegex.MatchString(dlURL) {
		downloadedPath, downloadErr := f.downloadFromTelegram(ctx, dlURL, path, pm)
		if downloadErr != nil {
			return "", downloadErr
		}
		if !fileExists(downloadedPath) {
			return "", errors.New("empty file returned by API")
		}
		return downloadedPath, nil
	}

	// Background disk-caching disabled: on an ephemeral filesystem (e.g.
	// Render's free tier), the downloads/ folder is wiped on every
	// restart, so the cache rarely survives long enough to be reused -
	// meanwhile this download still costs real CPU/bandwidth on an
	// already CPU-constrained host. If persistent disk is available,
	// this can be re-enabled: go f.cacheInBackground(dlURL, path)

	return dlURL, nil
}

// backgroundCacheDelay lets live playback have the CDN/bandwidth to itself
// for a while before the background cache download starts, instead of both
// competing for bandwidth at the same time right as playback begins.
const backgroundCacheDelay = 20 * time.Second

// cacheInBackground downloads a track to disk after playback has already
// started streaming from the CDN URL, so subsequent plays hit the local
// cache instead of re-fetching from the API. It waits backgroundCacheDelay
// first so it doesn't compete with the live stream for bandwidth.
func (f *FallenApiPlatform) cacheInBackground(dlURL, path string) {
	time.Sleep(backgroundCacheDelay)

	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := f.downloadFromURL(bgCtx, dlURL, path); err != nil {
		gologging.Debug("FallenApi: background cache failed -> " + err.Error())
		return
	}
	gologging.Debug("FallenApi: background cache complete -> " + path)
}

func (f *FallenApiPlatform) getDownloadURL(
	ctx context.Context,
	mediaURL string,
) (string, error) {
	apiReqURL := fmt.Sprintf(
		"%s/api/track?api_key=%s&url=%s",
		config.FallenAPIURL,
		config.FallenAPIKey,
		url.QueryEscape(mediaURL),
	)

	var apiResp apiResponse

	resp, err := rc.R().
		SetContext(ctx).
		SetResult(&apiResp).
		Get(apiReqURL)
	if err != nil {
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}

		return "", fmt.Errorf(
			"failed to download %s, api request failed: %w", mediaURL,
			sanitizeAPIError(err, config.FallenAPIKey),
		)
	}

	if resp.StatusCode() >= 400 {
		err = sanitizeAPIError(fmt.Errorf(
			"failed to download %s, api request failed with status: %d body: %s",
			mediaURL,
			resp.StatusCode(),
			resp.String(),
		), config.FallenAPIKey)
		gologging.Error(err.Error())
		return "", err
	}

	if apiResp.CdnUrl == "" {
		err = sanitizeAPIError(fmt.Errorf(
			"failed to download %s, empty API response body: %s",
			mediaURL,
			resp.String(),
		), config.FallenAPIKey)
		gologging.Error(err.Error())
		return "", err
	}

	return apiResp.CdnUrl, nil
}

func (f *FallenApiPlatform) downloadFromURL(
	ctx context.Context,
	dlURL, path string,
) error {
	resp, err := rc.R().
		SetContext(ctx).
		Get(dlURL)
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

	return nil
}

func (f *FallenApiPlatform) downloadFromTelegram(
	ctx context.Context,
	dlURL, path string,
	pm *telegram.ProgressManager,
) (string, error) {
	matches := telegramDLRegex.FindStringSubmatch(dlURL)
	if len(matches) < 3 {
		return "", fmt.Errorf("invalid telegram download url: %s", dlURL)
	}

	username := matches[1]
	messageID, err := strconv.Atoi(matches[2])
	if err != nil {
		return "", fmt.Errorf("invalid message ID: %v", err)
	}

	msg, err := core.Bot.GetMessageByID(username, int32(messageID))
	if err != nil {
		return "", fmt.Errorf("failed to fetch Telegram message: %w", err)
	}

	dOpts := &telegram.DownloadOptions{
		FileName: path,
		Ctx:      ctx,
	}
	if pm != nil {
		dOpts.ProgressManager = pm
	}
	_, err = msg.Download(dOpts)
	if err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

