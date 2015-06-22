package main

import (
	"archive/zip"
	"encoding/binary"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var commentPattern = regexp.MustCompile(`zipfs:(\S+)\s+(\S+)`)

func main() {
	var sourceDir string

	flag.StringVar(&sourceDir, "src", "", "Root source directory")
	flag.Usage = func() {
		fmt.Printf("Usage: %s [options] <executable-file>\n", filepath.Base(os.Args[0]))
		flag.PrintDefaults()
	}
	flag.Parse()

	exePath := flag.Arg(0)
	if exePath == "" {
		flag.Usage()
		os.Exit(1)
	} else if _, err := os.Stat(exePath); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Executable file \"%s\" does not exist\n", exePath)
		os.Exit(1)
	}

	sourceDir = getSourceDir(sourceDir)
	parseTree(sourceDir, exePath)
}

func getSourceDir(srcDir string) string {
	if srcDir != "" {
		if _, err := os.Stat(srcDir); err == nil {
			return srcDir
		} else {
			fmt.Fprintf(os.Stderr, "ERROR: %s does not exist\n", srcDir)
			os.Exit(1)
		}
	}

	binDir := filepath.Dir(os.Args[0])
	cwd, _ := os.Getwd()
	dirs := []string{
		cwd,
		binDir,
		filepath.Join(binDir, ".."),
	}

	for _, dir := range dirs {
		dir := filepath.Join(dir, "src")
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}

	fmt.Fprintln(os.Stderr, "ERROR: Could not find src directory")
	os.Exit(1)
	return ""
}

func parseTree(dir string, exePath string) {
	exeName := filepath.Base(exePath)
	exeName = strings.TrimSuffix(exeName, filepath.Ext(exeName))

	fset := token.NewFileSet()
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if filepath.Ext(path) == ".go" {
			file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				return err
			}

			binaryName := filepath.Base(filepath.Dir(path))
			if file.Name.Name != "main" || binaryName == exeName {
				for _, s := range file.Comments {
					for _, cmt := range s.List {
						if strings.HasPrefix(cmt.Text, "//") {
							handleComment(cmt.Text, path, exePath)
						}
					}
				}
			}
		}
		return nil
	})
}

func handleComment(comment string, filePath string, exePath string) {
	matches := commentPattern.FindStringSubmatch(comment)
	if matches != nil {
		collectionName := matches[1]
		dataDir := filepath.Join(filepath.Dir(filePath), matches[2])
		if _, err := os.Stat(dataDir); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Directory \"%s\" does not exist\n", dataDir)
			os.Exit(1)
		}

		title := fmt.Sprintf("Collection \"%s\"", collectionName)
		fmt.Println(title + "\n" + strings.Repeat("-", len(title)))

		err := appendZipData(exePath, collectionName, dataDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Could not append zip data: %s\n", err)
			os.Exit(1)
		}
		fmt.Println()
	}
}

func appendZipData(exePath string, collectionName string, dataDir string) error {
	dataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(exePath, os.O_APPEND|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer file.Close()

	offset, err := file.Seek(0, os.SEEK_END)
	if err != nil {
		return err
	}

	file.WriteString(collectionName)
	file.Write([]byte{0})

	zip := zip.NewWriter(file)

	err = filepath.Walk(dataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			relPath := strings.TrimLeft(strings.TrimPrefix(path, dataDir), "/")
			fmt.Println(relPath)

			reader, err := os.Open(path)
			if err != nil {
				return err
			}
			defer reader.Close()

			writer, err := zip.Create(relPath)
			if err != nil {
				return err
			}

			_, err = io.Copy(writer, reader)
			if err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		zip.Close()
		return err
	}

	zip.Close()
	file.WriteString("ZIPR")
	binary.Write(file, binary.BigEndian, int32(offset))

	return nil
}
