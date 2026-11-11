package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

// trashRun is the process boundary. Tests replace it and never call a real trash command.
var trashRun = func(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("system trash: %w: %s", err, out)
	}
	return nil
}

func systemTrash(path string) error { return trashFile(runtime.GOOS, path, trashRun) }

func trashFile(platform, path string, run func(string, ...string) error) error {
	switch platform {
	case "darwin":
		// argv keeps quotes, newlines and shell characters in filenames as data.
		return run("osascript", "-e", "on run argv\n tell application \"Finder\" to delete POSIX file (item 1 of argv)\nend run", path)
	case "linux":
		return run("gio", "trash", "--", path)
	default:
		return fmt.Errorf("system trash is not supported on %s; the file was not deleted", platform)
	}
}
