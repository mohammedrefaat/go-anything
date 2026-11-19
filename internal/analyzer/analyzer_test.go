package analyzer_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mohammedrefat/go-anything/internal/analyzer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzer_AnalyzeCurrentRepo(t *testing.T) {
	a := analyzer.New()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Analyze pkg/adapterkit
	res, err := a.Analyze(ctx, "../../pkg/adapterkit", ".")
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "github.com/mohammedrefat/go-anything", res.ModulePath)

	pkg, ok := res.Packages["github.com/mohammedrefat/go-anything/pkg/adapterkit"]
	require.True(t, ok, "adapterkit package should be found")
	assert.Equal(t, "adapterkit", pkg.Name)
	assert.NotEmpty(t, pkg.Files)
	assert.NotEmpty(t, pkg.PublicAPIs)
	assert.NotEmpty(t, pkg.Types)

	assert.Contains(t, pkg.Types, "Unit")
	assert.Contains(t, pkg.Types, "Case")
	assert.Contains(t, pkg.Types, "Evidence")
	assert.Contains(t, pkg.PublicAPIs, "type Unit")
}

func TestAnalyzer_MultiPackageWithImports(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ga-analyzer-pkg-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create a 2-package module: pkg/util and pkg/service
	goMod := `module testexample.com/myapp
go 1.22
`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(goMod), 0644))

	utilDir := filepath.Join(tempDir, "pkg", "util")
	require.NoError(t, os.MkdirAll(utilDir, 0755))
	utilSrc := `package util

func FormatName(s string) string {
	return "name: " + s
}
`
	require.NoError(t, os.WriteFile(filepath.Join(utilDir, "util.go"), []byte(utilSrc), 0644))

	svcDir := filepath.Join(tempDir, "pkg", "service")
	require.NoError(t, os.MkdirAll(svcDir, 0755))
	svcSrc := `package service

import "testexample.com/myapp/pkg/util"

type Service struct{}

func (s *Service) Hello(name string) string {
	return util.FormatName(name)
}
`
	require.NoError(t, os.WriteFile(filepath.Join(svcDir, "service.go"), []byte(svcSrc), 0644))

	a := analyzer.New()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	res, err := a.Analyze(ctx, tempDir, "./...")
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "testexample.com/myapp", res.ModulePath)
	assert.Len(t, res.Packages, 2)

	svcPkg := res.Packages["testexample.com/myapp/pkg/service"]
	require.NotNil(t, svcPkg)
	assert.Contains(t, svcPkg.InternalDeps, "testexample.com/myapp/pkg/util")
	assert.Contains(t, svcPkg.PublicAPIs, "type Service")
	assert.Contains(t, svcPkg.PublicAPIs, "method (Service).Hello")

	utilPkg := res.Packages["testexample.com/myapp/pkg/util"]
	require.NotNil(t, utilPkg)
	assert.Empty(t, utilPkg.InternalDeps)
	assert.Contains(t, utilPkg.PublicAPIs, "func FormatName")
}
