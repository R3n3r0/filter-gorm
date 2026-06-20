// Command filtergen generates filter structs (and optionally repository
// scaffolding) from GORM models.
//
// Usage:
//
//	filtergen -models ./models -out ./models/filter [-repos]
//
// It is designed to be used with go:generate, for example by adding the
// following directive next to your models:
//
//	//go:generate go run github.com/R3n3r0/filter-gorm/cmd/filtergen -models . -out ./filter -repos
//
// Generated *_gen.go files carry a "DO NOT EDIT" header and are meant to be
// regenerated; the repository implementation is a starting point you can adapt.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/R3n3r0/filter-gorm/internal/gen"
)

func main() {
	log.SetFlags(0)

	cfg := gen.Config{
		FilterHelperImport: "github.com/R3n3r0/filter-gorm/filter_helper",
	}

	flag.StringVar(&cfg.ModelsDir, "models", ".", "directory containing the GORM models")
	flag.StringVar(&cfg.ModelsImport, "models-import", "", "import path of the models package (auto-detected from go.mod when empty)")
	flag.StringVar(&cfg.OutDir, "out", "./filter", "output directory for the generated filters")
	flag.StringVar(&cfg.FilterPkg, "pkg", "filter", "package name for the generated filters")
	flag.StringVar(&cfg.FilterImportPath, "filter-import", "", "import path of the generated filters (auto-detected when empty)")
	flag.BoolVar(&cfg.GenerateRepos, "repos", false, "also generate repository scaffolding")
	flag.StringVar(&cfg.ReposDir, "repos-out", "./repository", "output directory for the generated repositories")
	flag.StringVar(&cfg.ReposPkg, "repos-pkg", "repository", "package name for the generated repositories")
	flag.StringVar(&cfg.FilterHelperImport, "filter-helper-import", cfg.FilterHelperImport, "import path of the filter_helper package")
	flag.Parse()

	if cfg.ModelsImport == "" {
		imp, err := detectImportPath(cfg.ModelsDir)
		if err != nil {
			log.Fatalf("filtergen: cannot detect models import path: %v\n"+
				"pass it explicitly with -models-import", err)
		}
		cfg.ModelsImport = imp
	}

	if cfg.GenerateRepos && cfg.FilterImportPath == "" {
		if imp, err := detectImportPath(cfg.OutDir); err == nil {
			cfg.FilterImportPath = imp
		}
	}

	if err := gen.Generate(cfg); err != nil {
		log.Fatalf("filtergen: %v", err)
	}

	fmt.Printf("filtergen: generated filters in %s\n", cfg.OutDir)
	if cfg.GenerateRepos {
		fmt.Printf("filtergen: generated repositories in %s\n", cfg.ReposDir)
	}
}

// detectImportPath resolves the import path of dir by locating the enclosing
// go.mod and joining the module path with the relative directory.
func detectImportPath(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	root := abs
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", fmt.Errorf("no go.mod found above %s", abs)
		}
		root = parent
	}

	module, err := moduleParse(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return module, nil
	}
	return module + "/" + rel, nil
}

func moduleParse(goMod string) (string, error) {
	file, err := os.Open(goMod)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module")), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("module directive not found in %s", goMod)
}
