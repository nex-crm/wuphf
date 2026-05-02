package team

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func (b *Broker) handleMemory(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
		query := strings.TrimSpace(r.URL.Query().Get("query"))
		keyFilter := strings.TrimSpace(r.URL.Query().Get("key"))
		limit := 5
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		// Snapshot under the lock: `mem := b.sharedMemory` only copies the
		// map header, leaving readers below racing concurrent POST writes
		// for the same outer/inner maps. searchPrivateMemory iterates the
		// entry maps and json.Encoder serializes the whole tree, both of
		// which can panic with "concurrent map iteration and map write".
		b.mu.Lock()
		mem := make(map[string]map[string]string, len(b.sharedMemory))
		for ns, entries := range b.sharedMemory {
			cloned := make(map[string]string, len(entries))
			for k, v := range entries {
				cloned[k] = v
			}
			mem[ns] = cloned
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if namespace != "" {
			entries := mem[namespace]
			switch {
			case keyFilter != "":
				var payload []brokerMemoryEntry
				if raw, ok := entries[keyFilter]; ok {
					payload = append(payload, brokerEntryFromNote(decodePrivateMemoryNote(keyFilter, raw)))
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"namespace": namespace,
					"entries":   payload,
				})
				return
			case query != "":
				matches := searchPrivateMemory(entries, query, limit)
				payload := make([]brokerMemoryEntry, 0, len(matches))
				for _, note := range matches {
					payload = append(payload, brokerEntryFromNote(note))
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"namespace": namespace,
					"entries":   payload,
				})
				return
			default:
				matches := searchPrivateMemory(entries, "", len(entries))
				payload := make([]brokerMemoryEntry, 0, len(matches))
				for _, note := range matches {
					payload = append(payload, brokerEntryFromNote(note))
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"namespace": namespace,
					"entries":   payload,
				})
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"memory": mem})
	case http.MethodPost:
		var body struct {
			Namespace string `json:"namespace"`
			Key       string `json:"key"`
			Value     any    `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		ns := strings.TrimSpace(body.Namespace)
		key := strings.TrimSpace(body.Key)
		if ns == "" || key == "" {
			http.Error(w, "namespace and key required", http.StatusBadRequest)
			return
		}
		b.mu.Lock()
		if b.sharedMemory == nil {
			b.sharedMemory = make(map[string]map[string]string)
		}
		if b.sharedMemory[ns] == nil {
			b.sharedMemory[ns] = make(map[string]string)
		}
		value := ""
		switch typed := body.Value.(type) {
		case string:
			value = typed
		default:
			data, err := json.Marshal(typed)
			if err != nil {
				b.mu.Unlock()
				http.Error(w, "invalid value", http.StatusBadRequest)
				return
			}
			value = string(data)
		}
		b.sharedMemory[ns][key] = value
		if err := b.saveLocked(); err != nil {
			b.mu.Unlock()
			http.Error(w, "failed to persist", http.StatusInternalServerError)
			return
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "namespace": ns, "key": key})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
