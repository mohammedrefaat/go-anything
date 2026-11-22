// Package adapterkit defines the public, transport-agnostic interface and data
// contracts for language adapters and application runners in go-anything.
//
// Stability Notice:
// In accordance with ADRs and the v3 specification, pkg/adapterkit and the
// matching v1 schemas are frozen and STABLE as of v1.0. Breaking changes
// are prohibited; backwards compatibility is strictly maintained.
//
// Architectural Invariants (Rule 8):
//  1. pkg/adapterkit must NEVER import any internal/ package.
//  2. Redactor and ShadowPolicy are core-owned invariants and must NEVER be exported from this package.
//  3. Adapters describe data semantics (identifying secret, volatile, or ordered fields); Core owns redaction, sandbox boundaries, and shadow policy.
package adapterkit
