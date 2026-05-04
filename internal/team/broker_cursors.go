package team

// Persistence cursors for incremental sync. Two cursors:
//   - notificationSince: how far the broker has scanned for
//     notification fan-out (so a restart doesn't replay every old
//     message into nex)
//   - insightsSince: how far insight extraction has scanned
//
// Both are "high-water mark" timestamps — the SetXxx writers
// monotonically advance and persist via saveLocked; the read paths
// just return the current value under the mutex.

func (b *Broker) NotificationCursor() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.notificationSince
}

func (b *Broker) SetNotificationCursor(cursor string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if cursor == "" || cursor == b.notificationSince {
		return nil
	}
	b.notificationSince = cursor
	return b.saveLocked()
}

func (b *Broker) InsightsCursor() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.insightsSince
}

func (b *Broker) SetInsightsCursor(cursor string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if cursor == "" || cursor == b.insightsSince {
		return nil
	}
	b.insightsSince = cursor
	return b.saveLocked()
}
