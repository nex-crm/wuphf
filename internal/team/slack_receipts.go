package team

import (
	"strings"
	"time"
)

const maxSlackReceiptEntries = 1000

func (b *Broker) slackEventSeenOrMark(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.slackEvents == nil {
		b.slackEvents = make(map[string]string)
	}
	if _, ok := b.slackEvents[id]; ok {
		return true
	}
	b.slackEvents[id] = time.Now().UTC().Format(time.RFC3339)
	b.pruneSlackReceiptsLocked()
	_ = b.saveLocked()
	return false
}

func (b *Broker) recordSlackOutbound(messageID, channelID, ts string) {
	messageID = strings.TrimSpace(messageID)
	channelID = strings.TrimSpace(channelID)
	ts = strings.TrimSpace(ts)
	if messageID == "" || channelID == "" || ts == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.slackOutbound == nil {
		b.slackOutbound = make(map[string]slackOutboundReceipt)
	}
	b.slackOutbound[messageID] = slackOutboundReceipt{
		MessageID: messageID,
		ChannelID: channelID,
		Timestamp: ts,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	b.pruneSlackReceiptsLocked()
	_ = b.saveLocked()
}

func (b *Broker) slackOutboundTimestamp(messageID string) string {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.slackOutbound == nil {
		return ""
	}
	return b.slackOutbound[messageID].Timestamp
}

func (b *Broker) pruneSlackReceiptsLocked() {
	pruneStringMapByOldest(b.slackEvents, maxSlackReceiptEntries)
	if len(b.slackOutbound) <= maxSlackReceiptEntries {
		return
	}
	items := make([]receiptItem, 0, len(b.slackOutbound))
	for key, receipt := range b.slackOutbound {
		items = append(items, receiptItem{key: key, at: receipt.CreatedAt})
	}
	pruneReceiptKeys(items, len(b.slackOutbound)-maxSlackReceiptEntries, func(key string) { delete(b.slackOutbound, key) })
}

type receiptItem struct {
	key string
	at  string
}

func pruneStringMapByOldest(values map[string]string, max int) {
	if len(values) <= max {
		return
	}
	items := make([]receiptItem, 0, len(values))
	for key, at := range values {
		items = append(items, receiptItem{key: key, at: at})
	}
	pruneReceiptKeys(items, len(values)-max, func(key string) { delete(values, key) })
}

func pruneReceiptKeys(items []receiptItem, count int, remove func(string)) {
	for i := 0; i < count; i++ {
		oldest := -1
		for j, item := range items {
			if item.key == "" {
				continue
			}
			if oldest == -1 {
				oldest = j
				continue
			}
			if item.at < items[oldest].at {
				oldest = j
			}
		}
		if oldest == -1 {
			return
		}
		remove(items[oldest].key)
		items[oldest] = receiptItem{}
	}
}
