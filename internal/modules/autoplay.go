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
	"strings"

	tg "github.com/amarnathcjd/gogram/telegram"

	"main/internal/core"
	state "main/internal/core/models"
	"main/internal/database"
	"main/internal/locales"
	"main/internal/platforms"
	"main/internal/utils"
)

func init() {
	helpTexts["/autoplay"] = `<i>Automatically keep playing similar tracks when the queue ends.</i>

<u>Usage:</u>
<b>/autoplay</b> — Shows current autoplay status (enabled/disabled).
<b>/autoplay enable</b> — Enable autoplay.
<b>/autoplay disable</b> — Disable autoplay.

<b>🧠 Details:</b>
When enabled, if the queue finishes with nothing left to play, the bot
searches for a track similar to the last one played (based on its title)
and keeps the music going automatically.

<b>⚠️ Restrictions:</b>
This command can only be used by chat admins.`
}

func autoplayHandler(m *tg.NewMessage) error {
	args := strings.Fields(m.Text())
	chatID := m.ChannelID()

	current, err := database.Autoplay(chatID)
	if err != nil {
		return err
	}

	if len(args) < 2 {
		m.Reply(F(chatID, "autoplay_status", locales.Arg{
			"action": F(chatID, utils.IfElse(current, "enabled", "disabled")),
		}))
		return tg.ErrEndGroup
	}

	enabled, err := utils.ParseBool(args[1])
	if err != nil {
		m.Reply(F(chatID, "invalid_bool"))
		return tg.ErrEndGroup
	}

	if enabled == current {
		m.Reply(F(chatID, "autoplay_already", locales.Arg{
			"action": F(chatID, utils.IfElse(enabled, "enabled", "disabled")),
		}))
		return tg.ErrEndGroup
	}

	if err := database.SetAutoplay(chatID, enabled); err != nil {
		return err
	}

	m.Reply(F(chatID, "autoplay_updated", locales.Arg{
		"action": F(chatID, utils.IfElse(enabled, "enabled", "disabled")),
	}))
	return tg.ErrEndGroup
}

// autoplayNextTrack finds a track similar to the last one played in the
// given room, to keep music going when the queue runs out. It returns nil
// if autoplay is disabled for the chat, there's no track to base the
// search on, or no suitable track is found.
func autoplayNextTrack(chatID int64, r *core.RoomState) *state.Track {
	enabled, err := database.Autoplay(chatID)
	if err != nil || !enabled {
		return nil
	}

	last := r.Track()
	if last == nil || last.Title == "" {
		return nil
	}

	query := cleanAutoplayQuery(last.Title)
	if query == "" {
		return nil
	}

	tracks, err := platforms.SearchQuery(query, false)
	if err != nil || len(tracks) == 0 {
		return nil
	}

	for _, track := range tracks {
		if track == nil || track.ID == last.ID {
			continue
		}
		track.Requester = F(chatID, "autoplay_requester")
		return track
	}

	return nil
}

// cleanAutoplayQuery strips common noise (official video/audio tags,
// bracketed text, featuring credits) from a track title so the search
// is more likely to surface a genuinely similar track rather than the
// exact same upload.
func cleanAutoplayQuery(title string) string {
	noise := []string{
		"(Official Video)", "(Official Audio)", "(Official Music Video)",
		"[Official Video]", "[Official Audio]", "(Lyrics)", "(Lyric Video)",
		"(Audio)", "(Visualizer)", "(HD)", "(4K)",
	}
	cleaned := title
	for _, n := range noise {
		cleaned = strings.ReplaceAll(cleaned, n, "")
	}
	return strings.TrimSpace(cleaned)
}
