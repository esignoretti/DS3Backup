package main

import (
	"fmt"
	"context"
	"github.com/esignoretti/ds3backup/internal/s3client"
	"github.com/esignoretti/ds3backup/internal/config"
	"strings"
)

func main() {
	cfg, _ := config.LoadConfig("/Users/esignoretti/ds3backup.ok/config.json")
	s3c, _ := s3client.NewClient(cfg.S3)
	ctx := context.Background()
	jobID := "job_1777400107082669000"
	
	fmt.Println("🔍 Checking S3 bucket:", cfg.S3.Bucket)
	fmt.Println()
	
	// Check central index
	fmt.Println("1. Central Index: .ds3backup/index/" + jobID + "/")
	files, _ := s3c.ListObjects(ctx, ".ds3backup/index/" + jobID + "/")
	if len(files) == 0 {
		fmt.Println("   ❌ EMPTY - No central index files!")
	} else {
		fmt.Printf("   ✅ Found %d files:\n", len(files))
		for _, f := range files {
			fmt.Println("      •", f)
		}
	}
	
	// Check backup snapshots
	fmt.Println("\n2. Backup Snapshots: backups/" + jobID + "/")
	snaps, _ := s3c.ListObjects(ctx, "backups/" + jobID + "/")
	if len(snaps) == 0 {
		fmt.Println("   ❌ No snapshots!")
	} else {
		fmt.Printf("   ✅ Found %d total files\n", len(snaps))
		// Group by directory
		dirs := make(map[string][]string)
		for _, s := range snaps {
			parts := strings.Split(s, "/")
			if len(parts) >= 3 {
				dir := parts[2]
				dirs[dir] = append(dirs[dir], s)
			}
		}
		for dir, files := range dirs {
			fmt.Printf("\n   📁 %s (%d files)\n", dir, len(files))
			for _, f := range files[:min(3, len(files))] {
				fmt.Println("      •", f)
			}
			if len(files) > 3 {
				fmt.Printf("      ... and %d more\n", len(files)-3)
			}
		}
	}
}

func min(a, b int) int {
	if a < b { return a }
	return b
}
