package main

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

type WorkerResult struct {
	Status string `json:"status"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func taskCalcStats(payload map[string]any) (any, error) {
	rawNums, ok := payload["numbers"]
	if !ok || rawNums == nil {
		return nil, fmt.Errorf("numbers list required")
	}
	slice, ok := rawNums.([]any)
	if !ok || len(slice) == 0 {
		return nil, fmt.Errorf("numbers list required")
	}

	numbers := make([]float64, 0, len(slice))
	for _, v := range slice {
		switch n := v.(type) {
		case float64:
			numbers = append(numbers, n)
		case int:
			numbers = append(numbers, float64(n))
		default:
			return nil, fmt.Errorf("numbers must be numeric")
		}
	}

	sort.Float64s(numbers)
	sum := 0.0
	minVal := numbers[0]
	maxVal := numbers[len(numbers)-1]
	for _, n := range numbers {
		sum += n
	}
	mean := sum / float64(len(numbers))

	var median float64
	mid := len(numbers) / 2
	if len(numbers)%2 == 0 {
		median = (numbers[mid-1] + numbers[mid]) / 2.0
	} else {
		median = numbers[mid]
	}

	return map[string]any{
		"count":  len(numbers),
		"mean":   mean,
		"median": median,
		"min":    minVal,
		"max":    maxVal,
		"sum":    sum,
	}, nil
}

func taskHashPayload(payload map[string]any) (any, error) {
	rawData, ok := payload["data"]
	if !ok {
		return nil, fmt.Errorf("data string required")
	}
	data, ok := rawData.(string)
	if !ok {
		return nil, fmt.Errorf("data string required")
	}

	algo := "sha256"
	if a, ok := payload["algo"].(string); ok && a != "" {
		algo = a
	}

	b := []byte(data)
	var digest string
	switch algo {
	case "sha256":
		h := sha256.Sum256(b)
		digest = hex.EncodeToString(h[:])
	case "md5":
		h := md5.Sum(b)
		digest = hex.EncodeToString(h[:])
	case "sha1":
		h := sha1.Sum(b)
		digest = hex.EncodeToString(h[:])
	default:
		return nil, fmt.Errorf("unsupported algorithm: %s", algo)
	}

	return map[string]any{
		"algo":   algo,
		"digest": digest,
		"bytes":  len(b),
	}, nil
}

func taskFilterRecords(payload map[string]any) (any, error) {
	rawRecords, ok := payload["records"]
	if !ok {
		return nil, fmt.Errorf("records list required")
	}
	slice, ok := rawRecords.([]any)
	if !ok {
		return nil, fmt.Errorf("records list required")
	}

	minScore := 0.0
	if ms, ok := payload["min_score"].(float64); ok {
		minScore = ms
	}
	targetCat, hasCat := payload["category"].(string)

	var filtered []map[string]any
	for _, item := range slice {
		rec, ok := item.(map[string]any)
		if !ok {
			continue
		}
		score := 0.0
		if s, ok := rec["score"].(float64); ok {
			score = s
		}
		if score >= minScore {
			if !hasCat || rec["category"] == targetCat {
				filtered = append(filtered, rec)
			}
		}
	}

	return map[string]any{
		"total_input":    len(slice),
		"filtered_count": len(filtered),
		"results":        filtered,
	}, nil
}

func ProcessJob(taskName string, payload map[string]any) WorkerResult {
	var res any
	var err error

	switch taskName {
	case "calc_stats":
		res, err = taskCalcStats(payload)
	case "hash_payload":
		res, err = taskHashPayload(payload)
	case "filter_records":
		res, err = taskFilterRecords(payload)
	default:
		return WorkerResult{
			Status: "error",
			Error:  fmt.Sprintf("unknown task: %s", taskName),
		}
	}

	if err != nil {
		return WorkerResult{
			Status: "error",
			Error:  err.Error(),
		}
	}
	return WorkerResult{
		Status: "success",
		Result: res,
	}
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println(`{"error":"usage: worker <task_name> <json_payload>"}`)
		os.Exit(1)
	}
	taskName := os.Args[1]
	rawPayload := os.Args[2]

	var payload map[string]any
	if err := json.Unmarshal([]byte(rawPayload), &payload); err != nil {
		fmt.Printf(`{"status":"error","error":"malformed json: %v"}`+"\n", err)
		os.Exit(2)
	}

	output := ProcessJob(taskName, payload)
	outBytes, _ := json.Marshal(output)
	fmt.Println(string(outBytes))
}
