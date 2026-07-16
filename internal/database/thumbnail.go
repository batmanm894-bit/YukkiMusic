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

package database

// ThumbnailsDisabled returns whether thumbnails are disabled bot-wide.
func ThumbnailsDisabled(chatID int64) (bool, error) {
	state, err := getBotState()
	if err != nil {
		return false, err
	}
	return state.NoThumb, nil
}

// SetThumbnailsDisabled sets whether thumbnails should be disabled bot-wide.
func SetThumbnailsDisabled(chatID int64, disabled bool) error {
	return modifyBotState(func(s *BotState) bool {
		if s.NoThumb == disabled {
			return false
		}
		s.NoThumb = disabled
		return true
	})
}
