package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
)

type Item struct {
	ID      int     `json:"id"`
	Name    string  `json:"name"`
	Price   float64 `json:"price"`
	InStock bool    `json:"in_stock"`
}

type ItemStore struct {
	mu    sync.RWMutex
	items []Item
}

func NewItemStore() *ItemStore {
	return &ItemStore{
		items: []Item{
			{ID: 1, Name: "Widget A", Price: 19.99, InStock: true},
			{ID: 2, Name: "Widget B", Price: 29.50, InStock: false},
			{ID: 3, Name: "Gadget Pro", Price: 99.00, InStock: true},
		},
	}
}

func (s *ItemStore) List() []Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]Item, len(s.items))
	copy(res, s.items)
	return res
}

func (s *ItemStore) Get(id int) (*Item, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, it := range s.items {
		if it.ID == id {
			c := it
			return &c, true
		}
	}
	return nil, false
}

func (s *ItemStore) Create(name string, price float64, inStock bool) Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	maxID := 0
	for _, it := range s.items {
		if it.ID > maxID {
			maxID = it.ID
		}
	}
	newItem := Item{
		ID:      maxID + 1,
		Name:    name,
		Price:   price,
		InStock: inStock,
	}
	s.items = append(s.items, newItem)
	return newItem
}

func (s *ItemStore) Update(id int, patch map[string]any) (*Item, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, it := range s.items {
		if it.ID == id {
			if v, ok := patch["name"].(string); ok {
				s.items[i].Name = v
			}
			if v, ok := patch["price"].(float64); ok {
				s.items[i].Price = v
			}
			if v, ok := patch["in_stock"].(bool); ok {
				s.items[i].InStock = v
			}
			updated := s.items[i]
			return &updated, true, nil
		}
	}
	return nil, false, nil
}

func (s *ItemStore) Delete(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, it := range s.items {
		if it.ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			return true
		}
	}
	return false
}

func sendJSON(w http.ResponseWriter, status int, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

func SetupHandler(store *ItemStore) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			sendJSON(w, http.StatusNotFound, map[string]any{"error": "endpoint not found"})
			return
		}
		sendJSON(w, http.StatusOK, map[string]any{
			"status":  "ok",
			"service": "items-api",
		})
	})

	mux.HandleFunc("/items", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			sendJSON(w, http.StatusOK, store.List())
		case http.MethodPost:
			var body struct {
				Name    *string  `json:"name"`
				Price   *float64 `json:"price"`
				InStock *bool    `json:"in_stock"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				sendJSON(w, http.StatusBadRequest, map[string]any{"error": "malformed JSON body"})
				return
			}
			if body.Name == nil || body.Price == nil {
				sendJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid payload: name and price required"})
				return
			}
			inStock := false
			if body.InStock != nil {
				inStock = *body.InStock
			}
			created := store.Create(*body.Name, *body.Price, inStock)
			sendJSON(w, http.StatusCreated, created)
		default:
			sendJSON(w, http.StatusNotFound, map[string]any{"error": "endpoint not found"})
		}
	})

	mux.HandleFunc("/items/", func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.TrimPrefix(r.URL.Path, "/items/")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			sendJSON(w, http.StatusNotFound, map[string]any{"error": "item not found"})
			return
		}

		switch r.Method {
		case http.MethodGet:
			item, found := store.Get(id)
			if !found {
				sendJSON(w, http.StatusNotFound, map[string]any{"error": "item not found"})
				return
			}
			sendJSON(w, http.StatusOK, item)

		case http.MethodPut:
			var patch map[string]any
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				sendJSON(w, http.StatusBadRequest, map[string]any{"error": "malformed JSON body"})
				return
			}
			updated, found, _ := store.Update(id, patch)
			if !found {
				sendJSON(w, http.StatusNotFound, map[string]any{"error": "item not found"})
				return
			}
			sendJSON(w, http.StatusOK, updated)

		case http.MethodDelete:
			deleted := store.Delete(id)
			if !deleted {
				sendJSON(w, http.StatusNotFound, map[string]any{"error": "item not found"})
				return
			}
			sendJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})

		default:
			sendJSON(w, http.StatusNotFound, map[string]any{"error": "endpoint not found"})
		}
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, pattern := mux.Handler(r)
		if pattern == "" {
			sendJSON(w, http.StatusNotFound, map[string]any{"error": "endpoint not found"})
			return
		}
		h.ServeHTTP(w, r)
	})
}

func main() {
	port := "3000"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	store := NewItemStore()
	handler := SetupHandler(store)

	addr := "127.0.0.1:" + port
	fmt.Printf("Go items-api listening on http://%s\n", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
