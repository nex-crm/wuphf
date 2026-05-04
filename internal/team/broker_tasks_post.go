package team

import (
	"encoding/json"
	"log"
	"net/http"
)

func (b *Broker) handlePostTask(w http.ResponseWriter, r *http.Request) {
	var body TaskPostRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	result, err := b.MutateTask(body)
	if err != nil {
		writeTaskMutationHTTPError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Printf("tasks post: encode response: %v", err)
	}
}
