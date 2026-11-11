package main

import (
	"errors"
	"fmt"
	"os"
)

func writeNewDocument(path, text string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := f.WriteString(text)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return errors.Join(err, os.Remove(path))
	}
	return nil
}

// A hard link claims the destination without replacement. No copy fallback is
// used across filesystems; a failed unlink removes only the link just created.
func moveDocument(from, to string) error {
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s: moving a document requires a regular file", from)
	}
	if err := os.Link(from, to); err != nil {
		return err
	}
	if err := os.Remove(from); err != nil {
		return errors.Join(err, os.Remove(to))
	}
	return nil
}
