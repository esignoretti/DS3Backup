package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// CreateBackupArchive creates a tar.gz archive of the .ds3backup directory
func CreateBackupArchive(sourceDir, destPath string) error {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	// Create the archive file
	file, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Create gzip writer
	gw := gzip.NewWriter(file)
	defer gw.Close()

	// Create tar writer
	tw := tar.NewWriter(gw)
	defer tw.Close()

	// Walk the source directory
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if path == sourceDir {
			return nil
		}

		// Skip temp files and directories
		if strings.Contains(path, "/temp/") || strings.HasSuffix(path, ".tmp") {
			return nil
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		// Use relative path in archive - strip the top directory
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		// Normalize path separators for cross-platform compatibility
		header.Name = filepath.ToSlash(relPath)

		// Write header
		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// If it's a file, write its content
		if !info.IsDir() {
			data, err := os.Open(path)
			if err != nil {
				return err
			}
			defer data.Close()

			if _, err := io.Copy(tw, data); err != nil {
				return err
			}
		}

		return nil
	})
}

// ExtractBackupArchive extracts a tar.gz archive to the destination directory
func ExtractBackupArchive(source io.Reader, destDir string) error {
	// Ensure destination exists
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}

	// Create gzip reader
	gr, err := gzip.NewReader(source)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gr.Close()

	// Create tar reader
	tr := tar.NewReader(gr)

	// Extract files
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Security check: prevent directory traversal
		if strings.Contains(header.Name, "..") {
			return fmt.Errorf("invalid path in archive: %s", header.Name)
		}

		// Strip leading .ds3backup/ prefix if present (handles old archive format)
		name := header.Name
		name = strings.TrimPrefix(name, "./")
		name = strings.TrimPrefix(name, ".ds3backup/")
		if name == ".ds3backup" || name == "." {
			continue
		}

		// Build target path
		target := filepath.Join(destDir, filepath.FromSlash(name))

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}

		case tar.TypeReg:
			// Create file
			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

			if _, err := io.Copy(file, tr); err != nil {
				file.Close()
				return err
			}
			file.Close()

		default:
			// Skip other types (symlinks, devices, etc.)
			continue
		}
	}

	return nil
}

// CalculateSHA256 calculates SHA256 checksum of a file
func CalculateSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// CalculateSHA256FromReader calculates SHA256 checksum from a reader
func CalculateSHA256FromReader(r io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, r); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// VerifySHA256 verifies file checksum matches expected value
func VerifySHA256(filePath, expected string) error {
	actual, err := CalculateSHA256(filePath)
	if err != nil {
		return err
	}

	if actual != expected {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}

	return nil
}

// VerifySHA256FromReader verifies checksum from reader matches expected value
func VerifySHA256FromReader(r io.Reader, expected string) error {
	// We need to read twice - once for hash, once for content
	// TeeReader lets us do both in one pass
	var data []byte
	hash := sha256.New()
	tee := io.TeeReader(r, hash)
	
	data, err := io.ReadAll(tee)
	if err != nil {
		return err
	}

	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}

	// Restore reader with data
	_ = data // Data is consumed during extraction
	return nil
}
