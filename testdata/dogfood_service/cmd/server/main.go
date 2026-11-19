package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"example.com/payments/pkg/billing"
	"example.com/payments/pkg/currency"
)

type ConvertResponse struct {
	Amount    float64 `json:"amount"`
	From      string  `json:"from"`
	To        string  `json:"to"`
	Result    float64 `json:"result"`
	Formatted string  `json:"formatted"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func handleConvert(w http.ResponseWriter, r *http.Request) {
	amountStr := r.URL.Query().Get("amount")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")

	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(ErrorResponse{Error: "invalid amount"})
		return
	}

	result, err := currency.Convert(amount, from, to)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	formatted, err := currency.Format(result, to)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(ConvertResponse{
		Amount:    amount,
		From:      from,
		To:        to,
		Result:    result,
		Formatted: formatted,
	})
}

func handleCharge(w http.ResponseWriter, r *http.Request) {
	amountStr := r.URL.Query().Get("amount")
	curr := r.URL.Query().Get("currency")
	riskStr := r.URL.Query().Get("risk")

	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(ErrorResponse{Error: "invalid amount"})
		return
	}

	highRisk := riskStr == "true" || riskStr == "1"
	receipt, err := billing.ProcessPayment(amount, curr, highRisk)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(ErrorResponse{Error: err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"receipt": receipt})
}

func main() {
	port := "8080"
	if envPort := os.Getenv("PORT"); envPort != "" {
		port = envPort
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/convert", handleConvert)
	mux.HandleFunc("/charge", handleCharge)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	fmt.Printf("Payments service listening on :%s\n", port)
	_ = http.ListenAndServe(":"+port, mux)
}
