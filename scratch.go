package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/unixsuperhero/ochazuke/markdown"
)

// dataDir is where o-md keeps its data: --data, else $O_MD_DATA, else
// $XDG_DATA_HOME/o-md, else ~/.local/share/o-md (the XDG convention the
// family's state and config directories follow).
func dataDir(flag string) string {
	if flag != "" {
		return flag
	}
	if d := os.Getenv("O_MD_DATA"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "o-md")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "o-md")
	}
	return filepath.Join(home, ".local", "share", "o-md")
}

func scratchDir(data string) string { return filepath.Join(data, "scratch") }

// newScratchPath names a new scratch buffer's file by the time, never one
// that exists.
func newScratchPath(data string, now time.Time) string {
	base := now.UTC().Format("20060102T150405Z")
	path := filepath.Join(scratchDir(data), base+".md")
	for n := 2; exists(path); n++ {
		path = filepath.Join(scratchDir(data), base+"-"+strconv.Itoa(n)+".md")
	}
	return path
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// lazySource is a file that exists only once something is written to it:
// a missing file loads as empty, and the first save makes its directory.
func lazySource(path string) markdown.Source {
	return markdown.Source{
		Load: func() (string, error) {
			b, err := os.ReadFile(path)
			if errors.Is(err, fs.ErrNotExist) {
				return "", nil
			}
			return string(b), err
		},
		Save: func(text string) error {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			return markdown.WriteFile(path, text)
		},
	}
}

// scratchFile is one scratch buffer as o-md scratch lists it.
type scratchFile struct {
	Path     string    `json:"path"`
	Modified time.Time `json:"modified"`
	Size     int64     `json:"size"`
	First    string    `json:"first"` // its first nonblank line
}

// scratchFiles lists the scratch buffers, newest first.
func scratchFiles(data string) ([]scratchFile, error) {
	entries, err := os.ReadDir(scratchDir(data))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []scratchFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(scratchDir(data), e.Name())
		f := scratchFile{Path: path, Modified: info.ModTime(), Size: info.Size()}
		if b, err := os.ReadFile(path); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				if t := strings.TrimSpace(line); t != "" {
					f.First = t
					break
				}
			}
		}
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Modified.Equal(out[j].Modified) {
			return out[i].Modified.After(out[j].Modified)
		}
		return out[i].Path > out[j].Path
	})
	return out, nil
}
