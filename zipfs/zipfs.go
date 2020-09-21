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

var commentPattern = regexp.MustCompile(`zipfs:(\S+)\s+(\S+)((?:\s+-x\s*\S+)*)`)
var excludePattern = regexp.MustCompile(`-x\s*(\S+)`)

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
		_, _ = fmt.Fprintf(os.Stderr, "ERROR: Executable file \"%s\" does not exist\n", exePath)
		os.Exit(1)
	}

	if hasZipData(exePath) {
		_, _ = fmt.Fprintf(os.Stderr, "ERROR: Executable file \"%s\" already has zipped resource data appended\n", exePath)
		os.Exit(1)
	}

	sourceDir = getSourceDir(sourceDir)
	parseTree(sourceDir, exePath)
}

func getSourceDir(srcDir string) string {
	if srcDir != "" {
		if _, err := os.Stat(srcDir); err == nil {
			return srcDir
		}
		_, _ = fmt.Fprintf(os.Stderr, "ERROR: %s does not exist\n", srcDir)
		os.Exit(1)
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

	_, _ = fmt.Fprintln(os.Stderr, "ERROR: Could not find src directory")
	os.Exit(1)
	return ""
}

func parseTree(dir string, exePath string) {
	exeName := filepath.Base(exePath)
	exeName = strings.TrimSuffix(exeName, filepath.Ext(exeName))

	fset := token.NewFileSet()
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if filepath.Ext(path) == ".go" {
			file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				return err
			}

			for _, s := range file.Comments {
				for _, cmt := range s.List {
					if strings.HasPrefix(cmt.Text, "//") {
						handleComment(cmt.Text, path, exePath)
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
		wd, _ := os.Getwd()
		dataDir := filepath.Join(filepath.Dir(filePath), matches[2])
		dataDir2 := filepath.Join(wd, matches[2])

		if _, err := os.Stat(dataDir); err != nil {
			if _, err := os.Stat(dataDir2); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "ERROR: Neither the directory \"%s\" nor \"%s\" does exist\n", dataDir, dataDir2)
				os.Exit(1)
			}
			dataDir = dataDir2
		}

		var excludes []string
		excludeMatches := excludePattern.FindAllStringSubmatch(matches[3], -1)
		for _, match := range excludeMatches {
			excludes = append(excludes, match[1])
		}

		fmt.Printf("Collection \"%s\":\n", collectionName)

		err := appendZipData(exePath, collectionName, dataDir, excludes)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "ERROR: Could not append zip data: %s\n", err)
			os.Exit(1)
		}
		fmt.Println()
	}
}

func hasZipData(exePath string) bool {
	file, err := os.Open(exePath)
	if err != nil {
		return false
	}

	data := make([]byte, 4)
	_, _ = file.Seek(-8, io.SeekEnd)
	_, _ = file.Read(data)

	return string(data) == "ZIPR"
}

func appendZipData(exePath string, collectionName string, dataDir string, excludes []string) error {
	dataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(exePath, os.O_APPEND|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	offset, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}

	_, _ = file.WriteString(collectionName)
	_, _ = file.Write([]byte{0})

	zipWriter := zip.NewWriter(file)

	err = filepath.Walk(dataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			stat, err := os.Stat(path)
			if err != nil {
				return err
			}

			relPath := strings.TrimLeft(filepath.ToSlash(strings.TrimPrefix(path, dataDir)), "/")
			include := true
			for _, exclude := range excludes {
				if exclude[0] == '/' && strings.HasPrefix(relPath, exclude[1:]) || strings.Contains(relPath, exclude) {
					include = false
					break
				}
			}

			if include {
				fmt.Println("- " + relPath)

				reader, err := os.Open(path)
				if err != nil {
					return err
				}
				defer reader.Close()

				header, err := zip.FileInfoHeader(stat)
				if err != nil {
					return err
				}
				header.Name = relPath

				writer, err := zipWriter.CreateHeader(header)
				if err != nil {
					return err
				}

				_, err = io.Copy(writer, reader)
				if err != nil {
					return err
				}
			}
		}

		return nil
	})

	if err != nil {
		_ = zipWriter.Close()
		return err
	}

	_ = zipWriter.Close()
	_, _ = file.WriteString("ZIPR")
	_ = binary.Write(file, binary.BigEndian, int32(offset))

	return nil
}
