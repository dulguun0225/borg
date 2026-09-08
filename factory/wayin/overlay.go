package wayin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// OverlayName is the name [Overlay] gives the JSON file "go build -overlay"
// is handed.
const OverlayName = "overlay.json"

// overlay is the shape "go build -overlay" reads: one field, mapping a path
// on disk to the file the build reads in its place. A path that exists
// nowhere on disk is how a file is added to a package, which is what the way
// in is: source the factory ships, compiled into the service's build and
// written into no checkout.
type overlay struct {
	Replace map[string]string `json:"Replace"`
}

// Overlay writes the shipped source, filled with identity, into dir, and
// returns the path of the overlay file naming it. checkout is the
// repository the build runs in: the overlay maps [FileName] in that
// directory, where no such file exists, to the one written in dir, so the
// build compiles the way in and the checkout is never written to.
//
// dir is the caller's to make and to remove, and is never inside checkout.
// There is no fallback: a build the overlay fails is a failed build,
// reported the way any other is.
func Overlay(checkout, dir, identity string) (string, error) {
	source, err := Source(identity)
	if err != nil {
		return "", err
	}
	inCheckout, err := filepath.Abs(filepath.Join(checkout, FileName))
	if err != nil {
		return "", fmt.Errorf("wayin: resolving where the way in goes in %s: %w", checkout, err)
	}
	written, err := filepath.Abs(filepath.Join(dir, FileName))
	if err != nil {
		return "", fmt.Errorf("wayin: resolving where to write the way in in %s: %w", dir, err)
	}
	// Both paths are absolute: the go command resolves a relative one
	// against its own working directory, which is the checkout at one build
	// site and this process's at the other.
	if err := os.WriteFile(written, []byte(source), 0o644); err != nil {
		return "", fmt.Errorf("wayin: writing the way in: %w", err)
	}
	encoded, err := json.Marshal(overlay{Replace: map[string]string{inCheckout: written}})
	if err != nil {
		return "", fmt.Errorf("wayin: encoding the overlay: %w", err)
	}
	path := filepath.Join(dir, OverlayName)
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return "", fmt.Errorf("wayin: writing the overlay: %w", err)
	}
	return path, nil
}
