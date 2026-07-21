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

package core

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Laky-64/gologging"

	state "main/internal/core/models"
	"main/ntgcalls"
)

const (
	minSpeed         = 0.50
	maxSpeed         = 4.0
	seekEndThreshold = 10
	seekSafetyMargin = 5
)

// Play starts playback of a track
func (r *RoomState) Play(t *state.Track, path string, force ...bool) error {
	if r.IsDestroyed() {
		return ErrRoomDestroyed
	}

	forcePlay := len(force) > 0 && force[0]

	r.mu.Lock()
	if r.Data != nil {
		delete(r.Data, "last_queue")
		delete(r.Data, "bar_style")
	}

	shouldQueue := !forcePlay && r.playing && r.track != nil
	if shouldQueue {
		r.queue = append(r.queue, t)
		r.mu.Unlock()
		return nil
	}

	if r.track != t {
		r.loop = 0
	}
	r.track = t
	r.playing = true
	r.filePath = path
	r.position = 0
	r.paused = false
	r.muted = false
	r.updatedAt = time.Now().Unix()
	r.mu.Unlock()

	err := r.play()
	if err != nil {
		r.mu.Lock()
		r.track = nil
		r.playing = false
		r.filePath = ""
		r.mu.Unlock()
		return err
	}

	return nil
}

// Pause pauses playback with optional auto-resume
func (r *RoomState) Pause(autoResumeAfter ...time.Duration) (bool, error) {
	if r.IsDestroyed() {
		return false, ErrRoomDestroyed
	}

	r.mu.RLock()
	alreadyPaused := r.paused
	r.mu.RUnlock()

	if alreadyPaused {
		return true, nil
	}

	paused, err := r.Assistant.Ntg.Pause(r.ID)
	if err != nil {
		return false, err
	}

	r.mu.RLock()
	isMuted := r.muted
	r.mu.RUnlock()

	if isMuted {
		r.Unmute()
	}

	r.mu.Lock()
	r.updatePosition()
	r.paused = true
	r.muted = false

	if r.scheduledTimers == nil {
		r.scheduledTimers = &scheduledTimers{}
	}
	r.scheduledTimers.cancelScheduledResume()

	if len(autoResumeAfter) > 0 && autoResumeAfter[0] > 0 {
		d := autoResumeAfter[0]
		r.scheduledResumeUntil = time.Now().Add(d)
		r.scheduledResumeTimer = time.AfterFunc(d, func() {
			if !r.IsDestroyed() {
				r.Resume()
			}
		})
	}
	r.mu.Unlock()

	return paused, nil
}

// Resume resumes playback
func (r *RoomState) Resume() (bool, error) {
	if r.IsDestroyed() {
		return false, ErrRoomDestroyed
	}

	if !r.IsActiveChat() {
		return false, fmt.Errorf("there are no active music playing")
	}

	r.mu.RLock()
	alreadyPlaying := !r.paused
	r.mu.RUnlock()

	if alreadyPlaying {
		return true, nil
	}

	resumed, err := r.Assistant.Ntg.Resume(r.ID)
	if err != nil {
		return false, err
	}

	r.mu.Lock()
	r.paused = false
	r.muted = false
	r.playing = true
	r.updatedAt = time.Now().Unix()
	if r.scheduledTimers != nil {
		r.scheduledTimers.cancelScheduledResume()
	}
	r.mu.Unlock()

	return resumed, nil
}

// Replay restarts the current track
func (r *RoomState) Replay() error {
	if r.IsDestroyed() {
		return ErrRoomDestroyed
	}

	r.mu.RLock()
	hasTrack := r.track != nil && r.filePath != ""
	r.mu.RUnlock()

	if !hasTrack {
		return fmt.Errorf("no track to replay")
	}

	r.mu.Lock()
	oldPos := r.position
	r.position = 0
	r.mu.Unlock()

	err := r.play()
	if err != nil {
		r.mu.Lock()
		r.position = oldPos
		r.mu.Unlock()
		return err
	}

	r.mu.Lock()
	r.position = 0
	r.paused = false
	r.muted = false
	r.playing = true
	r.updatedAt = time.Now().Unix()
	if r.scheduledTimers != nil {
		r.scheduledTimers.cancelScheduledResume()
		r.scheduledTimers.cancelScheduledUnmute()
	}
	r.mu.Unlock()

	return nil
}

// Stop stops playback completely
func (r *RoomState) Stop() error {
	if r.IsDestroyed() {
		return ErrRoomDestroyed
	}

	_, file, line, _ := runtime.Caller(1)
	gologging.DebugF("Stop Called from %s:%d", file, line)

	err := r.Assistant.Ntg.Stop(r.ID)

	r.mu.Lock()
	r.track = nil
	r.position = 0
	r.playing = false
	r.paused = false
	r.muted = false
	r.updatedAt = 0
	if r.scheduledTimers != nil {
		r.scheduledTimers.cancelScheduledUnmute()
		r.scheduledTimers.cancelScheduledResume()
		r.scheduledTimers.cancelScheduledSpeed()
	}
	r.mu.Unlock()

	return err
}

// Seek moves playback position by specified seconds
func (r *RoomState) Seek(seconds int) error {
	if r.IsDestroyed() {
		return ErrRoomDestroyed
	}

	r.mu.Lock()
	if r.track == nil || r.filePath == "" {
		r.mu.Unlock()
		return fmt.Errorf("no track to seek")
	}

	r.updatePosition()

	if seconds > 0 && r.track.Duration-r.position <= seekEndThreshold {
		r.mu.Unlock()
		return fmt.Errorf("cannot seek, track is about to end")
	}

	snapshot := struct {
		position int
		paused   bool
		muted    bool
		updated  int64
	}{
		position: r.position,
		paused:   r.paused,
		muted:    r.muted,
		updated:  r.updatedAt,
	}

	newPos := r.position + seconds
	if newPos >= r.track.Duration {
		newPos = r.track.Duration - seekSafetyMargin
	}
	if newPos < 0 {
		newPos = 0
	}

	r.position = newPos
	r.paused = false
	r.muted = false
	r.updatedAt = time.Now().Unix()
	r.mu.Unlock()

	err := r.play()
	if err != nil {
		r.mu.Lock()
		r.position = snapshot.position
		r.paused = snapshot.paused
		r.muted = snapshot.muted
		r.updatedAt = snapshot.updated
		r.mu.Unlock()
		return err
	}

	r.mu.RLock()
	wasMuted := snapshot.muted
	r.mu.RUnlock()

	if wasMuted {
		r.Assistant.Ntg.Unmute(r.ID)
	}

	return nil
}

// SetSpeed adjusts playback speed with optional auto-reset
func (r *RoomState) SetSpeed(
	speed float64,
	timeAfterNormal ...time.Duration,
) error {
	if r.IsDestroyed() {
		return ErrRoomDestroyed
	}

	r.mu.RLock()
	hasTrack := r.track != nil && r.filePath != ""
	currentSpeed := r.speed
	r.mu.RUnlock()

	if !hasTrack {
		return fmt.Errorf("no track to adjust speed")
	}

	if speed < minSpeed || speed > maxSpeed {
		return fmt.Errorf(
			"invalid speed: must be between %.2fx and %.1fx",
			minSpeed,
			maxSpeed,
		)
	}

	if currentSpeed == speed {
		return nil
	}

	r.mu.Lock()
	r.updatePosition()
	r.speed = speed
	r.playing = true
	r.paused = false
	r.muted = false
	r.updatedAt = time.Now().Unix()
	r.mu.Unlock()

	err := r.play()
	if err != nil {
		return err
	}

	r.mu.Lock()
	if r.scheduledTimers == nil {
		r.scheduledTimers = &scheduledTimers{}
	}
	r.scheduledTimers.cancelScheduledSpeed()

	shouldSchedule := len(timeAfterNormal) > 0 && timeAfterNormal[0] > 0 &&
		speed != 1.0
	if shouldSchedule {
		d := timeAfterNormal[0]
		r.scheduledSpeedUntil = time.Now().Add(d)
		r.scheduledSpeedTimer = time.AfterFunc(d, func() {
			r.resetSpeedToNormal()
		})
	}
	r.mu.Unlock()

	return nil
}

func (r *RoomState) resetSpeedToNormal() {
	if r.IsDestroyed() {
		return
	}

	r.mu.Lock()
	if r.track == nil || !r.playing || r.speed == 1.0 {
		r.mu.Unlock()
		return
	}

	r.updatePosition()
	r.speed = 1.0
	r.updatedAt = time.Now().Unix()
	r.mu.Unlock()

	r.play()
}

// Mute mutes playback with optional auto-unmute
func (r *RoomState) Mute(unmuteAfter ...time.Duration) (bool, error) {
	if r.IsDestroyed() {
		return false, ErrRoomDestroyed
	}

	r.mu.RLock()
	alreadyMuted := r.muted
	r.mu.RUnlock()

	if alreadyMuted {
		return true, nil
	}

	muted, err := r.Assistant.Ntg.Mute(r.ID)
	if err != nil {
		return false, err
	}

	r.mu.RLock()
	isPaused := r.paused
	r.mu.RUnlock()

	if isPaused {
		r.Resume()
	} else {
		r.Parse()
	}

	r.mu.Lock()
	r.muted = true
	if r.scheduledTimers == nil {
		r.scheduledTimers = &scheduledTimers{}
	}
	r.scheduledTimers.cancelScheduledUnmute()

	if len(unmuteAfter) > 0 && unmuteAfter[0] > 0 {
		duration := unmuteAfter[0]
		r.scheduledUnmuteUntil = time.Now().Add(duration)
		r.scheduledUnmuteTimer = time.AfterFunc(duration, func() {
			if !r.IsDestroyed() {
				r.Parse()
				r.Unmute()
			}
		})
	}
	r.mu.Unlock()

	return muted, nil
}

// Unmute unmutes playback
func (r *RoomState) Unmute() (bool, error) {
	if r.IsDestroyed() {
		return false, ErrRoomDestroyed
	}

	unmuted, err := r.Assistant.Ntg.Unmute(r.ID)
	if err != nil {
		return false, err
	}

	r.mu.Lock()
	r.updatePosition()
	r.muted = false
	r.paused = false
	if r.scheduledTimers != nil {
		r.scheduledTimers.cancelScheduledUnmute()
	}
	r.mu.Unlock()

	return unmuted, nil
}

func (r *RoomState) play() error {
	desc := getMediaDescription(r.filePath, r.position, r.speed, r.track.Video, r.track.Title)
	return r.Assistant.Ntg.Play(r.ID, desc)
}

func getMediaDescription(
	url string,
	pos int,
	speed float64,
	isVideo bool,
	title string,
) ntgcalls.MediaDescription {
	if speed < 0.5 {
		speed = 0.5
	} else if speed > 4.0 {
		speed = 4.0
	}

	baseCmd := "ffmpeg "
	if isStreamURL(url) {
		baseCmd += "-reconnect 1 -reconnect_streamed 1 -reconnect_delay_max 5 "
	}
	if pos > 0 {
		baseCmd += "-ss " + strconv.Itoa(pos) + " "
	}
	baseCmd += "-v warning -i \"" + url + "\" "

	audio := getAudioPipeline(baseCmd, speed, title)
	if !isVideo {
		return ntgcalls.MediaDescription{
			Microphone: audio,
		}
	}

	video := getVideoPipeline(baseCmd, url, speed)
	return ntgcalls.MediaDescription{
		Microphone: audio,
		Camera:     video,
	}
}

// audioEnhanceFilter applies loudness normalization tuned to match what
// Spotify and YouTube target on their own streams (-14 LUFS integrated,
// -1 dBTP true peak) instead of the more conservative broadcast standard,
// so tracks feel comparably loud/clean to those platforms. It's always
// applied last, after any mood-specific coloring below - and by itself
// (when no mood match is found) it IS the "clean, Spotify-style" sound:
// no extra bass/treble coloring, just a clean, consistently-loud master.
const audioEnhanceFilter = "loudnorm=I=-14:TP=-1:LRA=11"

// highQualityResample runs audio through the soxr resampler (a
// significantly higher-quality resampling algorithm than ffmpeg's
// default) before anything else touches it, reducing the small amount of
// artifacting that speed/EQ/loudnorm filters can otherwise introduce.
const highQualityResample = "aresample=resampler=soxr:precision=28"

// moodFilter does a best-effort guess at a track's vibe from its title
// text alone (title usually already includes the artist/song name, e.g.
// "DAKU | INDERPAL MOGA | ..." - no separate artist field exists). If a
// genre/mood is recognized, it returns an extra ffmpeg filter fragment to
// color the sound accordingly. If nothing matches, it returns "" and the
// track just gets the clean Spotify-style loudnorm treatment above with
// no extra coloring - this is the intentional fallback, not a missing
// feature. This is a coarse heuristic: it will miss plenty of romantic/
// attitude/Punjabi tracks whose titles don't happen to contain a
// matching word, in which case they also get the clean fallback sound.
func moodFilter(title string) string {
	t := strings.ToLower(title)

	switch {
	case containsAny(t, punjabiKeywords):
		// Energetic, dhol-driven: heavy low end, bright top end.
		return "bass=g=5:f=60:width_type=o:w=1,treble=g=3:f=9000:width_type=o:w=1"

	case containsAny(t, romanticKeywords):
		// Soft and warm: gentle low boost, tamed highs, light reverb-like echo.
		return "bass=g=3:f=100:width_type=o:w=1,treble=g=-2:f=8000:width_type=o:w=1,aecho=0.6:0.4:35:0.2"

	case containsAny(t, attitudeKeywords):
		// Punchy and forward: tight bass, presence boost, a bit of compression.
		return "bass=g=2:f=80:width_type=o:w=1,treble=g=2:f=5000:width_type=o:w=1,acompressor=threshold=-18dB:ratio=3:attack=5:release=50"

	default:
		// Nothing recognized - clean Spotify-style sound, no extra coloring.
		return ""
	}
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

var (
	punjabiKeywords = []string{
		"punjabi", "bhangra", "jatt", "sardar", "pind", "moga",
		"chandigarh", "desi hip hop",
	}
	romanticKeywords = []string{
		"pyar", "pyaar", "ishq", "mohabbat", "sanam", "romantic",
		"love song", "yaad", "judaai", "mohabbatein", "dil",
	}
	attitudeKeywords = []string{
		"attitude", "boss", "daku", "gunda", "villain", "swag",
		"sherni", "dabangg", "gangster",
	}
)

func getAudioPipeline(
	baseCmd string,
	speed float64,
	title string,
) *ntgcalls.AudioDescription {
	audio := &ntgcalls.AudioDescription{
		MediaSource:  ntgcalls.MediaSourceShell,
		SampleRate:   96000,
		ChannelCount: 2,
	}

	filterChain := highQualityResample + ",atempo=" + strconv.FormatFloat(speed, 'f', 2, 64)
	if extra := moodFilter(title); extra != "" {
		filterChain += "," + extra
	}
	filterChain += "," + audioEnhanceFilter

	audioCmd := baseCmd
	audioCmd += "-filter:a \"" + filterChain + "\" "
	audioCmd += "-f s16le -ac " + strconv.Itoa(int(audio.ChannelCount)) + " "
	audioCmd += "-ar " + strconv.Itoa(int(audio.SampleRate)) + " "
	audioCmd += "pipe:1"
	audio.Input = audioCmd

	return audio
}

func getVideoPipeline(
	baseCmd string,
	url string,
	speed float64,
) *ntgcalls.VideoDescription {
	w, h, fps, filter := normalizeVideo(url, speed)

	video := &ntgcalls.VideoDescription{
		MediaSource: ntgcalls.MediaSourceShell,
		Width:       int16(w),
		Height:      int16(h),
		Fps:         uint8(fps),
	}

	videoCmd := baseCmd
	videoCmd += "-filter:v \"" + filter + "\" "
	videoCmd += "-f rawvideo -r " + strconv.Itoa(fps) + " -pix_fmt yuv420p "
	videoCmd += "pipe:1"
	video.Input = videoCmd

	return video
}
