# Semantic Pitfalls & Traps When Porting to Go

When porting applications from dynamically-typed or managed languages (Node.js, TypeScript, Python) to Go, subtle behavioral divergences often arise. `go-anything` detects and tests for these key traps.

---

## 1. Empty Collections: `nil` vs `[]`

### The Trap
In Go:
- `var items []string` serializes to JSON `null`.
- `items := []string{}` serializes to JSON `[]`.

In JavaScript/Python, empty arrays are always `[]` or `list()`. Returning `nil` in Go can break upstream consumers expecting an array.

### Best Practice
Explicitly initialize empty slices when serializing to JSON:
```go
items := make([]Item, 0) // serializes to []
```

---

## 2. HTML Escaping in `encoding/json`

### The Trap
Go's standard `json.Marshal` escapes characters `<`, `>`, and `&` to `\u003c`, `\u003e`, `\u0026` to prevent XSS vulnerabilities in web pages. Python's `json.dumps()` and Node's `JSON.stringify()` do not escape these by default.

### Best Practice
When strict character-for-character JSON parity is required, use a custom `json.Encoder`:
```go
var buf bytes.Buffer
enc := json.NewEncoder(&buf)
enc.SetEscapeHTML(false)
_ = enc.Encode(data)
```

---

## 3. Map Key Order

### The Trap
- In Python 3.7+ and modern JavaScript (V8), dictionaries and object keys preserve insertion order.
- In Go, map iteration order is randomized by runtime design, and `json.Marshal` sorts map keys alphabetically.

### Best Practice
Do not rely on map key order for hash digests or comparison. When ordering matters, use ordered key-value slices or structs.

---

## 4. Number Precision & Types

### The Trap
- JavaScript uses IEEE 754 64-bit floats (`Number`) for all numbers. Large 64-bit integers (`int64` > $2^{53}-1$) lose precision in JSON unless transmitted as strings.
- Go decodes unconstrained JSON numbers into `float64` by default, which can cause precision loss on integers.

### Best Practice
Use `json.Decoder.UseNumber()` to preserve exact numeric representations as `json.Number`:
```go
dec := json.NewDecoder(r.Body)
dec.UseNumber()
```

---

## 5. String Offsets: UTF-8 vs UTF-16

### The Trap
- JavaScript strings are indexed by UTF-16 code units (e.g. `String.prototype.length` counts emojis as 2 units).
- Python strings are indexed by Unicode code points.
- Go strings are byte slices (`len(s)` returns byte count, not character count).

### Best Practice
When slicing strings or computing offsets:
```go
runes := []rune(str) // Unicode code points
// or utf8.RuneCountInString(str)
```

---

## 6. Error Handling & Exit Codes

### The Trap
- In Python and Node.js, uncaught exceptions bubble up and produce non-zero exit codes (often exit code 1).
- In Go, unhandled errors must be returned explicitly. A missing error check can lead to silent failures or unexpected zero exit codes.

### Best Practice
Always test both the positive (happy) and negative (error) paths. Use table-driven tests verifying status codes, error messages, and process exit codes.

---

## 7. Goroutine Leaks & Orphaned Goroutines

### The Trap
Spawning goroutines without lifecycle management or cancellation contexts can lead to background leaks and race conditions:
```go
go func() {
    // If parent request finishes, this may run forever or panic
}()
```

### Best Practice
Always tie concurrency to `context.Context` or use `sync.WaitGroup` / `errgroup.Group` with bounded concurrency:
```go
g, ctx := errgroup.WithContext(ctx)
g.SetLimit(runtime.NumCPU())
```
