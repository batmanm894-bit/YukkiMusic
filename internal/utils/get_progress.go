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

package utils

import (
	"fmt"
	"math/rand/v2"

	"github.com/amarnathcjd/gogram/telegram"
)

func GetProgress(statusMsg *telegram.NewMessage) *telegram.ProgressManager {
	pm := telegram.NewProgressManager(2)

	if statusMsg == nil {
		return pm
	}

	var opts *telegram.SendOptions
	if replyMarkup := statusMsg.ReplyMarkup(); replyMarkup != nil {
		opts = &telegram.SendOptions{ReplyMarkup: *replyMarkup}
	}

	pm.WithCallback(func(pi *telegram.ProgressInfo) {
		text := fmt.Sprintf(
			"<b>📥 Downloading your track...</b>\n"+
				"<pre>"+
				"Progress : %6.2f%%\n"+
				"Speed    : %s\n"+
				"Eta      : %s\n"+
				"Elapsed  : %s"+
				"</pre>",
			pi.Percentage,
			pi.SpeedString(),
			pi.ETAString(),
			pi.ElapsedString(),
		)
		statusMsg.Edit(text, opts)
	})

	return pm
}

var miniBarCache = [10]string{
	"▰▱▱▱▱▱▱▱▱▱",
	"▰▰▱▱▱▱▱▱▱▱",
	"▰▰▰▱▱▱▱▱▱▱",
	"▰▰▰▰▱▱▱▱▱▱",
	"▰▰▰▰▰▱▱▱▱▱",
	"▰▰▰▰▰▰▱▱▱▱",
	"▰▰▰▰▰▰▰▱▱▱",
	"▰▰▰▰▰▰▰▰▱▱",
	"▰▰▰▰▰▰▰▰▰▱",
	"▰▰▰▰▰▰▰▰▰▰",
}

// waveformBarCache mimics a WhatsApp voice-note waveform: the "played"
// portion shows waveform peaks, the remaining portion is a flat line.
var waveformBarCache = [10]string{
	"▂▬▬▬▬▬▬▬▬▬",
	"▂▄▬▬▬▬▬▬▬▬",
	"▂▄▆▬▬▬▬▬▬▬",
	"▂▄▆█▬▬▬▬▬▬",
	"▂▄▆█▆▬▬▬▬▬",
	"▂▄▆█▆▄▬▬▬▬",
	"▂▄▆█▆▄▂▬▬▬",
	"▂▄▆█▆▄▂▄▬▬",
	"▂▄▆█▆▄▂▄▆▬",
	"▂▄▆█▆▄▂▄▆█",
}

// GetProgressBar returns a progress bar for the given playback position.
// style selects which visual style to use: 0 = filled mini-bar,
// 1 = WhatsApp-style waveform. Callers should pick the style once per
// track (e.g. cache it on the room) rather than re-randomizing on every
// call, since this is re-rendered every few seconds while a track plays.
func GetProgressBar(playedSec, durationSec, style int) string {
	index := 0
	if durationSec > 0 && playedSec > 0 {
		if playedSec >= durationSec {
			index = 9
		} else {
			index = (playedSec * 10) / durationSec
			if index > 9 {
				index = 9
			}
		}
	}

	if style == 0 {
		return miniBarCache[index]
	}
	return waveformBarCache[index]
}

// RandomProgressBarStyle returns a random style index for GetProgressBar
// (0 or 1). Call this once per track and reuse the result.
func RandomProgressBarStyle() int {
	return rand.IntN(2)
}
