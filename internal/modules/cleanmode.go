package modules

import (
	"sync"
	"time"

	"github.com/Laky-64/gologging"
	tg "github.com/amarnathcjd/gogram/telegram"

	"main/internal/core"
	state "main/internal/core/models"
	"main/internal/database"
	"main/internal/locales"
	"main/internal/utils"
)

const (
	cleanModeBatchWindow = 10 * time.Second
	defaultCleanDelay    = 15 * time.Minute
)

var (
	cleanModeDurationOptions = []int{15, 30, 60, 5}
	cleanScheduler           = &CleanScheduler{pending: make(map[int64][]cleanEntry)}
)

func cleanModeReadHandler(u tg.Update, _ *tg.Client) error {
	upd, ok := u.(*tg.UpdateReadChannelOutbox)
	if !ok || upd.MaxID == 0 {
		return nil
	}

	chatID := int64(-1000000000000 - upd.ChannelID)
	if upd.MaxID == currentPlayingStatusMessageID(chatID) {
		return nil
	}

	cleanScheduler.schedule(chatID, upd.MaxID)
	return nil
}

func currentPlayingStatusMessageID(chatID int64) int32 {
	room, ok := core.GetRoom(chatID, nil, false)
	if !ok || room == nil {
		return 0
	}
	status := room.StatusMsg()
	if status == nil {
		return 0
	}
	return status.ID
}

func cleanModeDelay(chatID int64) time.Duration {
	settings, err := database.GetChatSettings(chatID)
	if err != nil || settings.CleanModeDurationMins <= 0 {
		return defaultCleanDelay
	}
	return time.Duration(settings.CleanModeDurationMins) * time.Minute
}

func cleanModeStatusText(chatID int64, enabled bool) string {
	settings, _ := database.GetChatSettings(chatID)
	duration := 15
	if settings != nil && settings.CleanModeDurationMins > 0 {
		duration = settings.CleanModeDurationMins
	}
	return F(
		chatID,
		"cleanmode_status",
		locales.Arg{"action": F(chatID, utils.IfElse(enabled, "enabled", "disabled")), "duration": duration},
	)
}

type cleanEntry struct {
	messageID int32
	dueAt     time.Time
}

type CleanScheduler struct {
	mu      sync.Mutex
	pending map[int64][]cleanEntry
}

func (s *CleanScheduler) start() {
	go func() {
		ticker := time.NewTicker(cleanModeBatchWindow)
		defer ticker.Stop()
		for range ticker.C {
			s.flushDue(time.Now())
		}
	}()
}

func (s *CleanScheduler) schedule(chatID int64, messageID int32) {
	if messageID == 0 {
		return
	}
	s.mu.Lock()
	s.pending[chatID] = append(s.pending[chatID], cleanEntry{
		messageID: messageID,
		dueAt:     time.Now().Add(cleanModeDelay(chatID)),
	})
	s.mu.Unlock()
}

func (s *CleanScheduler) cancel(chatID int64) {
	s.mu.Lock()
	delete(s.pending, chatID)
	s.mu.Unlock()
}

func (s *CleanScheduler) flushDue(deadline time.Time) {
	s.mu.Lock()
	batches := make(map[int64][]int32)

	for chatID, entries := range s.pending {
		statusID := currentPlayingStatusMessageID(chatID)
		keep := entries[:0]

		for _, entry := range entries {
			if entry.messageID == statusID || entry.dueAt.After(deadline) {
				keep = append(keep, entry)
				continue
			}
			batches[chatID] = append(batches[chatID], entry.messageID)
		}

		if len(keep) == 0 {
			delete(s.pending, chatID)
		} else {
			s.pending[chatID] = keep
		}
	}
	s.mu.Unlock()

	for chatID, ids := range batches {
		enabled, err := database.CleanMode(chatID)
		if err != nil || !enabled {
			continue
		}
		if _, err := core.Bot.DeleteMessages(chatID, ids); err != nil {
			gologging.DebugF("cleanmode delete failed chat=%d err=%v", chatID, err)
		}
	}
}

// deleteQueueMsg deletes the "Added to Queue: #N" message that was sent
// when this track was queued, now that the track is starting to play (or
// being skipped over) and that message no longer serves a purpose. No-op
// for tracks that were played immediately and never had a queue message
// (QueueMsgID == 0).
func deleteQueueMsg(chatID int64, t *state.Track) {
	if t == nil || t.QueueMsgID == 0 {
		return
	}
	id := t.QueueMsgID
	go func() {
		if _, err := core.Bot.DeleteMessages(chatID, []int32{id}); err != nil {
			gologging.DebugF("failed to delete queue message: %v", err)
		}
	}()
}

// scheduleOldPlayingMessage deletes the chat's previous "now playing" /
// "added to queue" status message as soon as it's about to be replaced by a
// new one (next track starting, skip, stop, etc). This runs unconditionally
// - independent of the user-configurable Clean Mode setting and its
// read-receipt-based delay (see CleanScheduler above), which exists for a
// different purpose: cleaning up command messages. The old status message
// is always stale the instant a new one is sent, so there's no reason to
// wait on it.
func scheduleOldPlayingMessage(r *core.RoomState) {
	if m := r.StatusMsg(); m != nil {
		go func() {
			if _, err := m.Delete(); err != nil {
				gologging.DebugF("failed to delete old status message: %v", err)
			}
		}()
	}
}
