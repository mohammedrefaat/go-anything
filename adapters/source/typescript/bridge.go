package typescript

import (
	"fmt"
)

// BridgeType identifies a foreign library bridging strategy between Node.js/TypeScript and Go.
type BridgeType string

const (
	// BridgeSubprocess routes foreign library calls through isolated subprocesses.
	BridgeSubprocess BridgeType = "subprocess"

	// BridgeHTTP routes foreign library calls over local loopback HTTP.
	BridgeHTTP BridgeType = "http"

	// BridgeKoffi uses the koffi Node.js C FFI library to dynamically link Go -buildmode=c-shared libraries.
	BridgeKoffi BridgeType = "koffi"

	// BridgeNAPI compiles native C/C++ Node-API addons that link Go C-archive or C-shared objects.
	BridgeNAPI BridgeType = "n-api"
)

// BridgeLimits declares the ABI, memory ownership, error mapping, and concurrency constraints of a bridge.
type BridgeLimits struct {
	Type              BridgeType `json:"type"`
	Name              string     `json:"name"`
	ProductionGrade   bool       `json:"production_grade"`
	ABICompatibility  string     `json:"abi_compatibility"`
	MemoryOwnership   string     `json:"memory_ownership"`
	ErrorMapping      string     `json:"error_mapping"`
	ConcurrencyModel  string     `json:"concurrency_model"`
	RequiredToolchain []string   `json:"required_toolchain"`
	Documentation     string     `json:"documentation"`
}

// GetBridgeDeclarations returns the formal constraints, guarantees, and trade-offs for all supported bridges.
func GetBridgeDeclarations() map[BridgeType]BridgeLimits {
	return map[BridgeType]BridgeLimits{
		BridgeSubprocess: {
			Type:              BridgeSubprocess,
			Name:              "Subprocess Wrapper",
			ProductionGrade:   true,
			ABICompatibility:  "JSON/Msgpack serialization over stdio pipes; zero binary ABI coupling.",
			MemoryOwnership:   "Complete process memory isolation; OS reclaims all resources on termination.",
			ErrorMapping:      "Exit codes and JSON error payload mapped to standard Go error return.",
			ConcurrencyModel:  "OS process scheduling; non-blocking asynchronous I/O via Node child_process / Go exec.",
			RequiredToolchain: []string{"node"},
			Documentation:     "Recommended for production strangler migrations. Crash-safe and zero binary ABI risks.",
		},
		BridgeHTTP: {
			Type:              BridgeHTTP,
			Name:              "Loopback HTTP Sidecar",
			ProductionGrade:   true,
			ABICompatibility:  "HTTP/1.1 and JSON payloads; decoupled client/server wire protocols.",
			MemoryOwnership:   "Independent address spaces; garbage collection isolated to respective runtimes.",
			ErrorMapping:      "HTTP 4xx/5xx status codes and RFC 7807 Problem Details mapped to Go errors.",
			ConcurrencyModel:  "Node.js event loop server concurrency paired with Go net/http goroutine pools.",
			RequiredToolchain: []string{"node"},
			Documentation:     "Recommended for microservices and decoupled unit strangler migrations.",
		},
		BridgeKoffi: {
			Type:              BridgeKoffi,
			Name:              "Koffi C-FFI Dynamic Link",
			ProductionGrade:   false,
			ABICompatibility:  "C ABI via libffi; requires Go compiled with -buildmode=c-shared.",
			MemoryOwnership:   "Callee-allocated buffers require explicit exported FreeString/FreeBuffer calls; V8 GC cannot manage Go heap.",
			ErrorMapping:      "Return structs containing error code and C string message, converted to JS Error objects.",
			ConcurrencyModel:  "Koffi async worker threads call into Go runtime; Cgo locks OS thread during foreign call.",
			RequiredToolchain: []string{"node", "npm:koffi", "gcc/clang", "cgo"},
			Documentation:     "High-performance in-process bridge for latency-critical paths. Requires careful memory management to prevent leaks.",
		},
		BridgeNAPI: {
			Type:              BridgeNAPI,
			Name:              "Node-API Native Addon",
			ProductionGrade:   false,
			ABICompatibility:  "Node-API (N-API) C ABI; version-independent ABI stability across Node releases.",
			MemoryOwnership:   "V8 handles wrapped via napi_ref and napi_create_reference; buffers passed via napi_get_buffer_info.",
			ErrorMapping:      "Go error strings mapped to napi_throw_error with custom error codes.",
			ConcurrencyModel:  "napi_threadsafe_function required for background Go goroutines calling into Node event loop.",
			RequiredToolchain: []string{"node", "node-gyp", "python3", "gcc/clang/msvc", "cgo"},
			Documentation:     "Standard Node native addon bridge. Fully supported for complex C++ or Go C-archive bindings.",
		},
	}
}

// ValidateBridge verifies whether the given bridge type is declared and returns its limits.
func ValidateBridge(b BridgeType) (BridgeLimits, error) {
	decls := GetBridgeDeclarations()
	limits, ok := decls[b]
	if !ok {
		return BridgeLimits{}, fmt.Errorf("unknown bridge type %q: supported types are %s, %s, %s, %s",
			b, BridgeSubprocess, BridgeHTTP, BridgeKoffi, BridgeNAPI)
	}
	return limits, nil
}

// GenerateKoffiBinding generates a JavaScript wrapper template for a Go c-shared library using koffi.
func GenerateKoffiBinding(libPath, functionName string, inputTypes, returnType string) string {
	return fmt.Sprintf(`// Generated Koffi FFI Bridge for %s
const koffi = require('koffi');
const path = require('path');

const lib = koffi.load(path.resolve(%q));
const %s = lib.func('%s', '%s', [%s]);

module.exports = {
  %s: function(...args) {
    return %s(...args);
  }
};
`, functionName, libPath, functionName, functionName, returnType, inputTypes, functionName, functionName)
}

// GenerateNAPIBinding generates a Node-API C++ stub declaration for bridging a Go exported function.
func GenerateNAPIBinding(functionName string) string {
	return fmt.Sprintf(`// Generated Node-API Binding Stub for %s
#include <node_api.h>

extern "C" {
    // Exported Go c-shared symbol
    int %s(const char* input, char** output);
}

napi_value Wrap_%s(napi_env env, napi_callback_info info) {
    size_t argc = 1;
    napi_value args[1];
    napi_get_cb_info(env, info, &argc, args, nullptr, nullptr);

    char inputBuf[1024];
    size_t strLen;
    napi_get_value_string_utf8(env, args[0], inputBuf, sizeof(inputBuf), &strLen);

    char* output = nullptr;
    int rc = %s(inputBuf, &output);
    if (rc != 0) {
        napi_throw_error(env, nullptr, "Go foreign function execution failed");
        return nullptr;
    }

    napi_value result;
    napi_create_string_utf8(env, output, NAPI_AUTO_LENGTH, &result);
    return result;
}
`, functionName, functionName, functionName, functionName)
}
