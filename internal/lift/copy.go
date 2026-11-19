package lift

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CopyDirectory recursively copies src directory to dst directory, skipping paths that match ignorePatterns.
func CopyDirectory(src, dst string, ignorePatterns []string) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)

	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("failed to stat source directory %s: %w", src, err)
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return fmt.Errorf("failed to create destination directory %s: %w", dst, err)
	}

	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path for %s: %w", path, err)
		}

		if relPath == "." {
			return nil
		}

		// Normalize to forward slashes for pattern matching
		normRel := filepath.ToSlash(relPath)
		for _, pattern := range ignorePatterns {
			if normRel == pattern || strings.HasPrefix(normRel, pattern+"/") {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		targetPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}

		// Copy regular file
		return copyFile(path, targetPath, info.Mode())
	})
}

// copyFile copies a single file from src to dst preserving permissions.
func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", dst, err)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", src, err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("failed to open destination file %s: %w", dst, err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file from %s to %s: %w", src, dst, err)
	}

	return nil
}

// RemovePathInDirectory safely removes a file or directory tree relative to rootDir.
// Returns list of successfully removed relative paths.
func RemovePathInDirectory(rootDir, relPath string) ([]string, error) {
	fullPath := filepath.Join(rootDir, filepath.FromSlash(relPath))
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return nil, nil
	}

	var removed []string
	err := filepath.Walk(fullPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() {
			if r, err := filepath.Rel(rootDir, path); err == nil {
				removed = append(removed, filepath.ToSlash(r))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := os.RemoveAll(fullPath); err != nil {
		return nil, fmt.Errorf("failed to remove %s: %w", fullPath, err)
	}

	return removed, nil
}
