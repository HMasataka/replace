package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
)

type Result struct {
	Path         string
	Replacements int
	Err          error
}

func main() {
	var (
		oldWord    string
		newWord    string
		targetPath string
		workers    int
		dryRun     bool
	)

	flag.StringVar(&oldWord, "old", "", "置換対象の単語 (必須)")
	flag.StringVar(&newWord, "new", "", "置換後の単語 (必須)")
	flag.StringVar(&targetPath, "path", "", "対象のファイルまたはディレクトリのパス (必須)")
	flag.IntVar(&workers, "workers", runtime.NumCPU(), "並列処理のワーカー数")
	flag.BoolVar(&dryRun, "dry-run", false, "実際には置換せず、対象ファイルを表示する")
	flag.Parse()

	if oldWord == "" || newWord == "" || targetPath == "" {
		fmt.Fprintln(os.Stderr, "エラー: -old, -new, -path は必須です")
		flag.Usage()
		os.Exit(1)
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: パスが見つかりません: %s\n", targetPath)
		os.Exit(1)
	}

	var files []string
	if info.IsDir() {
		files, err = collectFiles(targetPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "エラー: ディレクトリの走査に失敗しました: %v\n", err)
			os.Exit(1)
		}
	} else {
		files = []string{targetPath}
	}

	if len(files) == 0 {
		fmt.Println("対象ファイルがありません")
		return
	}

	results := processFiles(files, oldWord, newWord, workers, dryRun)

	printResults(results, dryRun)
}

func collectFiles(root string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		if isTextFile(path) {
			files = append(files, path)
		}

		return nil
	})

	return files, err
}

func isTextFile(path string) bool {
	ext := filepath.Ext(path)
	textExtensions := map[string]bool{
		".go": true, ".txt": true, ".md": true, ".json": true,
		".yaml": true, ".yml": true, ".xml": true, ".html": true,
		".css": true, ".js": true, ".ts": true, ".jsx": true,
		".tsx": true, ".py": true, ".rb": true, ".java": true,
		".c": true, ".cpp": true, ".h": true, ".hpp": true,
		".rs": true, ".sh": true, ".bash": true, ".zsh": true,
		".sql": true, ".graphql": true, ".proto": true,
		".toml": true, ".ini": true, ".conf": true, ".cfg": true,
		".env": true, ".gitignore": true, ".dockerfile": true,
		"": true,
	}

	return textExtensions[ext]
}

func processFiles(files []string, oldWord, newWord string, workers int, dryRun bool) []Result {
	fileChan := make(chan string, len(files))
	resultChan := make(chan Result, len(files))

	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range fileChan {
				result := replaceInFile(path, oldWord, newWord, dryRun)
				resultChan <- result
			}
		}()
	}

	for _, f := range files {
		fileChan <- f
	}
	close(fileChan)

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	var results []Result
	for result := range resultChan {
		results = append(results, result)
	}

	return results
}

func replaceInFile(path, oldWord, newWord string, dryRun bool) Result {
	content, err := os.ReadFile(path)
	if err != nil {
		return Result{Path: path, Err: err}
	}

	oldBytes := []byte(oldWord)
	newBytes := []byte(newWord)

	count := bytes.Count(content, oldBytes)
	if count == 0 {
		return Result{Path: path, Replacements: 0}
	}

	if dryRun {
		return Result{Path: path, Replacements: count}
	}

	newContent := bytes.ReplaceAll(content, oldBytes, newBytes)

	info, err := os.Stat(path)
	if err != nil {
		return Result{Path: path, Err: err}
	}

	err = os.WriteFile(path, newContent, info.Mode())
	if err != nil {
		return Result{Path: path, Err: err}
	}

	return Result{Path: path, Replacements: count}
}

func printResults(results []Result, dryRun bool) {
	var (
		totalFiles        int32
		totalReplacements int32
		errorCount        int32
	)

	fmt.Println()
	if dryRun {
		fmt.Println("=== ドライラン結果 ===")
	} else {
		fmt.Println("=== 置換結果 ===")
	}
	fmt.Println()

	for _, r := range results {
		if r.Err != nil {
			fmt.Printf("  [エラー] %s: %v\n", r.Path, r.Err)
			atomic.AddInt32(&errorCount, 1)
			continue
		}

		if r.Replacements > 0 {
			atomic.AddInt32(&totalFiles, 1)
			atomic.AddInt32(&totalReplacements, int32(r.Replacements))
			if dryRun {
				fmt.Printf("  [対象] %s (%d箇所)\n", r.Path, r.Replacements)
			} else {
				fmt.Printf("  [完了] %s (%d箇所置換)\n", r.Path, r.Replacements)
			}
		}
	}

	fmt.Println()
	fmt.Println("---")
	fmt.Printf("対象ファイル数: %d\n", totalFiles)
	fmt.Printf("置換箇所数: %d\n", totalReplacements)
	if errorCount > 0 {
		fmt.Printf("エラー数: %d\n", errorCount)
	}
}
