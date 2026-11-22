package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/pkg/adapterkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHelperProcess implements a mock foreign binary for subprocess wrapper testing.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}

	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "no command\n")
		os.Exit(2)
	}

	cmd := args[0]
	switch cmd {
	case "echo_json":
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read error: %v\n", err)
			os.Exit(1)
		}
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			fmt.Fprintf(os.Stderr, "json unmarshal error: %v\n", err)
			os.Exit(1)
		}
		parsed["_processed_by"] = "foreign_subprocess"
		out, _ := json.Marshal(parsed)
		os.Stdout.Write(out)

	case "echo_msgpack":
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(1)
		}
		var parsed any
		if err := DecodeMsgpack(data, &parsed); err != nil {
			os.Exit(1)
		}
		m, ok := parsed.(map[string]any)
		if !ok {
			m = make(map[string]any)
		}
		m["_processed_by"] = "foreign_subprocess"
		out, _ := EncodeMsgpack(m)
		os.Stdout.Write(out)

	case "echo_binary":
		data, _ := io.ReadAll(os.Stdin)
		res := append([]byte("SUBPROCESS:"), data...)
		os.Stdout.Write(res)

	case "sleep":
		time.Sleep(2 * time.Second)
		os.Stdout.WriteString(`{"status":"ok"}`)

	case "fail":
		fmt.Fprintf(os.Stderr, "intentional fatal error in foreign library")
		os.Exit(42)

	case "persistent_stream":
		// Persistent daemon reading lines and writing responses
		dec := json.NewDecoder(os.Stdin)
		enc := json.NewEncoder(os.Stdout)
		for {
			var msg map[string]any
			if err := dec.Decode(&msg); err != nil {
				break
			}
			msg["_server"] = "persistent"
			_ = enc.Encode(msg)
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown helper command: %s\n", cmd)
		os.Exit(1)
	}
}

func helperProcessCommand(t *testing.T, subcommand string, extraArgs ...string) SubprocessConfig {
	args := []string{"-test.run=TestHelperProcess", "--", subcommand}
	args = append(args, extraArgs...)
	return SubprocessConfig{
		Command: os.Args[0],
		Args:    args,
		Env: map[string]string{
			"GO_WANT_HELPER_PROCESS": "1",
		},
		Timeout: 5 * time.Second,
	}
}

func TestSubprocessWrapper_Formats(t *testing.T) {
	ctx := context.Background()

	t.Run("json_invocation", func(t *testing.T) {
		cfg := helperProcessCommand(t, "echo_json")
		cfg.Format = FormatJSON

		wrap, err := NewSubprocessWrapper(cfg)
		require.NoError(t, err)
		defer wrap.Close()

		input := map[string]any{"query": "antigravity", "limit": 10}
		out, err := wrap.Invoke(ctx, "search_unit", input)
		require.NoError(t, err)

		resMap, ok := out.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "antigravity", resMap["query"])
		assert.Equal(t, "foreign_subprocess", resMap["_processed_by"])
	})

	t.Run("msgpack_invocation", func(t *testing.T) {
		cfg := helperProcessCommand(t, "echo_msgpack")
		cfg.Format = FormatMsgpack

		wrap, err := NewSubprocessWrapper(cfg)
		require.NoError(t, err)
		defer wrap.Close()

		input := map[string]any{"action": "compress", "level": int64(9)}
		out, err := wrap.Invoke(ctx, "compress_unit", input)
		require.NoError(t, err)

		resMap, ok := out.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "compress", resMap["action"])
		assert.Equal(t, "foreign_subprocess", resMap["_processed_by"])
	})

	t.Run("binary_invocation", func(t *testing.T) {
		cfg := helperProcessCommand(t, "echo_binary")
		cfg.Format = FormatBinary

		wrap, err := NewSubprocessWrapper(cfg)
		require.NoError(t, err)
		defer wrap.Close()

		rawOut, err := wrap.InvokeRaw(ctx, "raw_unit", []byte("hello-bytes"))
		require.NoError(t, err)
		assert.Equal(t, "SUBPROCESS:hello-bytes", string(rawOut))
	})
}

func TestSubprocessWrapper_ErrorsAndTimeouts(t *testing.T) {
	ctx := context.Background()

	t.Run("timeout_error", func(t *testing.T) {
		cfg := helperProcessCommand(t, "sleep")
		cfg.Timeout = 100 * time.Millisecond

		wrap, err := NewSubprocessWrapper(cfg)
		require.NoError(t, err)
		defer wrap.Close()

		_, err = wrap.Invoke(ctx, "slow_unit", map[string]any{"data": 1})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrTimeout) || strings.Contains(err.Error(), "timed out"))
	})

	t.Run("process_failure_error_wrapping", func(t *testing.T) {
		cfg := helperProcessCommand(t, "fail")

		wrap, err := NewSubprocessWrapper(cfg)
		require.NoError(t, err)
		defer wrap.Close()

		_, err = wrap.Invoke(ctx, "failing_unit", map[string]any{"test": true})
		require.Error(t, err)

		var invocErr *InvocationError
		require.True(t, errors.As(err, &invocErr))
		assert.Equal(t, "failing_unit", invocErr.UnitID)
		assert.Equal(t, 42, invocErr.ExitCode)
		assert.Contains(t, invocErr.Stderr, "intentional fatal error")
	})
}

func TestSubprocessWrapper_PersistentLifecycle(t *testing.T) {
	ctx := context.Background()

	cfg := helperProcessCommand(t, "persistent_stream")
	cfg.Persistent = true
	cfg.Format = FormatJSON

	wrap, err := NewSubprocessWrapper(cfg)
	require.NoError(t, err)
	defer wrap.Close()

	err = wrap.Start(ctx)
	require.NoError(t, err)

	for i := 1; i <= 3; i++ {
		out, err := wrap.Invoke(ctx, "stream_unit", map[string]any{"counter": i})
		require.NoError(t, err)
		resMap, ok := out.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, float64(i), resMap["counter"])
		assert.Equal(t, "persistent", resMap["_server"])
	}

	err = wrap.Stop()
	require.NoError(t, err)
}

func TestHTTPWrapper(t *testing.T) {
	ctx := context.Background()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unit := strings.TrimPrefix(r.URL.Path, "/")
		if unit == "error_unit" {
			http.Error(w, "server side failure", http.StatusInternalServerError)
			return
		}

		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Content-Type") == "application/msgpack" {
			var decoded any
			_ = DecodeMsgpack(body, &decoded)
			m, _ := decoded.(map[string]any)
			m["_server"] = "foreign_http"
			m["_unit"] = unit
			resp, _ := EncodeMsgpack(m)
			w.Header().Set("Content-Type", "application/msgpack")
			w.Write(resp)
			return
		}

		if r.Header.Get("Content-Type") == "application/octet-stream" {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(append([]byte("HTTP:"), body...))
			return
		}

		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		parsed["_server"] = "foreign_http"
		parsed["_unit"] = unit
		out, _ := json.Marshal(parsed)
		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
	}))
	defer ts.Close()

	t.Run("http_json", func(t *testing.T) {
		wrap, err := NewHTTPWrapper(HTTPConfig{
			BaseURL: ts.URL,
			Format:  FormatJSON,
		})
		require.NoError(t, err)
		defer wrap.Close()

		out, err := wrap.Invoke(ctx, "user_service", map[string]any{"user_id": 42})
		require.NoError(t, err)
		resMap := out.(map[string]any)
		assert.Equal(t, "foreign_http", resMap["_server"])
		assert.Equal(t, "user_service", resMap["_unit"])
	})

	t.Run("http_msgpack", func(t *testing.T) {
		wrap, err := NewHTTPWrapper(HTTPConfig{
			BaseURL: ts.URL,
			Format:  FormatMsgpack,
		})
		require.NoError(t, err)
		defer wrap.Close()

		out, err := wrap.Invoke(ctx, "telemetry_unit", map[string]any{"event": "click"})
		require.NoError(t, err)
		resMap := out.(map[string]any)
		assert.Equal(t, "foreign_http", resMap["_server"])
		assert.Equal(t, "click", resMap["event"])
	})

	t.Run("http_binary", func(t *testing.T) {
		wrap, err := NewHTTPWrapper(HTTPConfig{
			BaseURL: ts.URL,
			Format:  FormatBinary,
		})
		require.NoError(t, err)
		defer wrap.Close()

		raw, err := wrap.InvokeRaw(ctx, "blob_unit", []byte("blob-content"))
		require.NoError(t, err)
		assert.Equal(t, "HTTP:blob-content", string(raw))
	})

	t.Run("http_error_wrapping", func(t *testing.T) {
		wrap, err := NewHTTPWrapper(HTTPConfig{
			BaseURL: ts.URL,
			Format:  FormatJSON,
		})
		require.NoError(t, err)
		defer wrap.Close()

		_, err = wrap.Invoke(ctx, "error_unit", map[string]any{"foo": "bar"})
		require.Error(t, err)
		var invocErr *InvocationError
		require.True(t, errors.As(err, &invocErr))
		assert.Equal(t, 500, invocErr.ExitCode)
		assert.Contains(t, invocErr.Stderr, "server side failure")
	})
}

// TestForeignLibrary_StranglerUnitByUnit proves migrating foreign library units to Go one unit at a time.
func TestForeignLibrary_StranglerUnitByUnit(t *testing.T) {
	ctx := context.Background()

	// Setup a mock foreign HTTP server representing the legacy Python library
	foreignServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unit := strings.TrimPrefix(r.URL.Path, "/")
		body, _ := io.ReadAll(r.Body)

		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)

		switch unit {
		case "markdown_parser":
			raw := fmt.Sprint(parsed["text"])
			parsed["html"] = "<p>" + raw + "</p>"
			parsed["engine"] = "legacy_python_markdown"
		case "data_cleaner":
			raw := fmt.Sprint(parsed["raw"])
			parsed["cleaned"] = strings.TrimSpace(strings.ToLower(raw))
			parsed["engine"] = "legacy_python_cleaner"
		default:
			parsed["engine"] = "legacy_unknown"
		}

		out, _ := json.Marshal(parsed)
		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
	}))
	defer foreignServer.Close()

	foreignWrap, err := NewHTTPWrapper(HTTPConfig{
		BaseURL: foreignServer.URL,
		Format:  FormatJSON,
	})
	require.NoError(t, err)
	defer foreignWrap.Close()

	router := NewRouter("library_migration")
	defer router.Close()

	// Step 1: Initially, register both units as foreign
	router.RegisterForeign("markdown_parser", foreignWrap)
	router.RegisterForeign("data_cleaner", foreignWrap)

	// Step 2: Test that both units execute foreign implementation
	out1, err := router.Invoke(ctx, "markdown_parser", map[string]any{"text": "Hello World"})
	require.NoError(t, err)
	res1 := out1.(map[string]any)
	assert.Equal(t, "<p>Hello World</p>", res1["html"])
	assert.Equal(t, "legacy_python_markdown", res1["engine"])

	out2, err := router.Invoke(ctx, "data_cleaner", map[string]any{"raw": "  FOO BAR  "})
	require.NoError(t, err)
	res2 := out2.(map[string]any)
	assert.Equal(t, "foo bar", res2["cleaned"])
	assert.Equal(t, "legacy_python_cleaner", res2["engine"])

	// Step 3: Migrate Unit 1 ("data_cleaner") to Go! Unit 2 ("markdown_parser") remains foreign.
	goCleaner := func(ctx context.Context, input any) (any, error) {
		m, ok := input.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid input")
		}
		raw := fmt.Sprint(m["raw"])
		return map[string]any{
			"cleaned": strings.TrimSpace(strings.ToLower(raw)),
			"engine":  "go_modern_cleaner",
		}, nil
	}
	router.MigrateUnit("data_cleaner", goCleaner)

	// Step 4: Verify Unit 1 now runs the Go implementation, and Unit 2 still runs foreign!
	out2Migrated, err := router.Invoke(ctx, "data_cleaner", map[string]any{"raw": "  NEW DATA  "})
	require.NoError(t, err)
	res2Migrated := out2Migrated.(map[string]any)
	assert.Equal(t, "new data", res2Migrated["cleaned"])
	assert.Equal(t, "go_modern_cleaner", res2Migrated["engine"])

	out1StillForeign, err := router.Invoke(ctx, "markdown_parser", map[string]any{"text": "Still Foreign"})
	require.NoError(t, err)
	res1StillForeign := out1StillForeign.(map[string]any)
	assert.Equal(t, "legacy_python_markdown", res1StillForeign["engine"])

	// Step 5: Test kill switch / fallback override: GO_ANYTHING_UNIT_DATA_CLEANER=original
	t.Setenv("GO_ANYTHING_UNIT_DATA_CLEANER", "original")
	assert.Equal(t, ModeForeign, router.GetEffectiveMode("data_cleaner"))

	fallbackOut, err := router.Invoke(ctx, "data_cleaner", map[string]any{"raw": "FALLBACK TEST"})
	require.NoError(t, err)
	resFallback := fallbackOut.(map[string]any)
	assert.Equal(t, "legacy_python_cleaner", resFallback["engine"])
	assert.Equal(t, int64(1), router.FallbackCount("data_cleaner"))

	// Clear fallback override
	t.Setenv("GO_ANYTHING_UNIT_DATA_CLEANER", "")

	// Step 6: Migrate Unit 2 ("markdown_parser") to Go!
	goMarkdown := func(ctx context.Context, input any) (any, error) {
		m, ok := input.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid input")
		}
		raw := fmt.Sprint(m["text"])
		return map[string]any{
			"html":   "<p>" + raw + "</p>",
			"engine": "go_modern_markdown",
		}, nil
	}
	router.MigrateUnit("markdown_parser", goMarkdown)

	// Verify both units are now running Go implementations
	out1Final, err := router.Invoke(ctx, "markdown_parser", map[string]any{"text": "Done"})
	require.NoError(t, err)
	assert.Equal(t, "go_modern_markdown", out1Final.(map[string]any)["engine"])

	out2Final, err := router.Invoke(ctx, "data_cleaner", map[string]any{"raw": "Final"})
	require.NoError(t, err)
	assert.Equal(t, "go_modern_cleaner", out2Final.(map[string]any)["engine"])

	// Step 7: Verify replay integration with adapterkit.Case
	replayCase := &adapterkit.Case{
		UnitID: "data_cleaner",
		Input: adapterkit.CaseInput{
			Body: map[string]any{"raw": "  Replay Test  "},
		},
	}
	caseOutput, err := router.ExecuteCase(ctx, replayCase)
	require.NoError(t, err)
	assert.Equal(t, 0, caseOutput.ExitCode)
	require.NotNil(t, caseOutput.Body)
	assert.Equal(t, "go_modern_cleaner", caseOutput.Body.(map[string]any)["engine"])
}
