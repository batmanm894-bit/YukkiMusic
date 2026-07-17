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

package cookies

import (
	"embed"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Laky-64/gologging"
	"resty.dev/v3"

	"main/internal/config"
)

const cookieDir = "internal/cookies"

var (
	cachedFiles []string
	cacheOnce   sync.Once
	client      = resty.New().SetTimeout(30 * time.Second)
)

//go:embed *.txt
var embeddedCookies embed.FS

func init() {
	gologging.Debug("🔹 Initializing cookies...")

	if err := copyEmbeddedCookies(); err != nil {
		gologging.Fatal("Failed to copy embedded cookies:", err)
	}
}

// Init downloads any cookie files configured via COOKIES_LINK.
//
// NOTE: this must be called explicitly *after* config.Load() has run
// (e.g. from platforms.Init()). It can't be done from this package's own
// init() because Go runs all package init() functions before main() calls
// config.Load() — at that point config.CookiesLink is always still empty,
// so COOKIES_LINK would silently never be fetched.
func Init() {
	urls := strings.Fields(config.CookiesLink)
	for _, url := range urls {
		if err := downloadCookieFile(url); err != nil {
			gologging.WarnF(
				"Failed to download cookie file from %s: %v",
				url,
				err,
			)
		}
	}
}

func copyEmbeddedCookies() error {
	if err := os.MkdirAll(cookieDir, 0o755); err != nil {
		return err
	}

	entries, err := embeddedCookies.ReadDir(".")
	if err != nil {
		return err
	}

	for _, e := range entries {

		if e.IsDir() || e.Name() == "example.txt" {
			continue
		}

		dst := filepath.Join(cookieDir, e.Name())

		if _, err := os.Stat(dst); err == nil {
			continue
		}

		data, err := embeddedCookies.ReadFile(e.Name())
		if err != nil {
			return err
		}

		if err := os.WriteFile(dst, data, 0o600); err != nil {
			return err
		}
	}

	return nil
}

func downloadCookieFile(url string) error {
	id := filepath.Base(url)
	rawURL := "https://batbin.me/raw/" + id
	filePath := filepath.Join(cookieDir, id+".txt")

	if err := os.MkdirAll(cookieDir, 0o755); err != nil {
		return fmt.Errorf("failed to create cookies dir: %w", err)
	}

	resp, err := client.R().Get(rawURL)
	if err != nil {
		return err
	}

	if resp.StatusCode() >= 400 {
		return fmt.Errorf(
			"unexpected status %d from %s",
			resp.StatusCode(),
			rawURL,
		)
	}

	return os.WriteFile(filePath, resp.Bytes(), 0o600)
}

func loadCookieCache() error {
	files, err := filepath.Glob(filepath.Join(cookieDir, "*.txt"))
	if err != nil {
		return err
	}

	var filtered []string

	for _, f := range files {
		if filepath.Base(f) == "example.txt" {
			continue
		}
		filtered = append(filtered, f)
	}

	cachedFiles = filtered
	return nil
}

func GetRandomCookieFile() (string, error) {
	var err error

	cacheOnce.Do(func() {
		err = loadCookieCache()
	})

	if err != nil {
		gologging.WarnF("Failed to load cookie cache: %v", err)
		return "", err
	}

	if len(cachedFiles) == 0 {
		gologging.Warn("No cookie files available")
		return "", nil
	}

	return cachedFiles[rand.Intn(len(cachedFiles))], nil
}

// GetRandomCookieHeader picks a random cookie file (same pool used for
// yt-dlp's --cookies) and turns its youtube.com/google.com entries into a
// ready-to-use "name=value; name2=value2" Cookie header string, for direct
// HTTP calls (e.g. the innertube API) that don't go through yt-dlp.
// Returns "" (no error) when no cookie files are configured.
func GetRandomCookieHeader() (string, error) {
	file, err := GetRandomCookieFile()
	if err != nil || file == "" {
		return "", err
	}

	return parseNetscapeCookieHeader(file)
}

// parseNetscapeCookieHeader reads a Netscape-format cookie file and returns
// its youtube.com/google.com cookies as a single "name=value; ..." header.
func parseNetscapeCookieHeader(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var pairs []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Netscape format: domain  flag  path  secure  expiry  name  value
		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			continue
		}

		domain := fields[0]
		if !strings.Contains(domain, "youtube.com") &&
			!strings.Contains(domain, "google.com") {
			continue
		}

		name := fields[5]
		value := fields[6]
		if name == "" {
			continue
		}

		pairs = append(pairs, name+"="+value)
	}

	return strings.Join(pairs, "; "), nil
}
