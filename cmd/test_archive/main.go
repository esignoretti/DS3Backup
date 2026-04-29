package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/esignoretti/ds3backup/internal/config"
	"github.com/esignoretti/ds3backup/internal/backup"
)

func main() {
	sourceDir, _ := config.ConfigDir()
	destPath := "/tmp/test_archive_out.tar.gz"
	
	fmt.Printf("Source dir: %s\n", sourceDir)
	
	fmt.Printf("Creating archive...\n")
	backup.CreateBackupArchive(sourceDir, destPath)
	
	fmt.Printf("Archive contents:\n")
	f, _ := os.Open(destPath)
	defer f.Close()
	gr, _ := gzip.NewReader(f)
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		h, err := tr.Next()
		if err == io.EOF { break }
		if err != nil { break }
		target := filepath.Join(sourceDir, h.Name)
		fmt.Printf("  %s → %s\n", h.Name, target)
		_ = "." // suppress unused warning
	}
}
