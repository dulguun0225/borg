package main

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dulguun0225/borg/factory/criterion"
)

// previousEmission derives the emission from the build currently deployed to
// production. The checkout is an archive of that build's recorded commit, so
// the criterion package applies the same derivation to both builds.
func (p *path) previousEmission(ctx context.Context, repo, serviceID string) (criterion.Emission, error) {
	previous, found := p.currentReleaseBuild(ctx, serviceID)
	if !found {
		return criterion.Emission{}, nil
	}
	checkout, err := archiveCheckout(ctx, repo, previous.CommitHash)
	if err != nil {
		return criterion.Emission{}, err
	}
	defer os.RemoveAll(checkout)
	derived, err := criterion.Derive(checkout)
	if err != nil {
		return criterion.Emission{}, err
	}
	return derived.Emission, nil
}

func archiveCheckout(ctx context.Context, repo, commit string) (string, error) {
	checkout, err := os.MkdirTemp("", "factory-emission-")
	if err != nil {
		return "", fmt.Errorf("factory: making the previous build checkout: %w", err)
	}
	archivePath := filepath.Join(checkout, "archive.tar")
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		os.RemoveAll(checkout)
		return "", fmt.Errorf("factory: making the previous build archive: %w", err)
	}
	command := exec.CommandContext(ctx, "git", "archive", "--format=tar", commit)
	command.Dir = repo
	command.Stdout = archiveFile
	if err := command.Run(); err != nil {
		archiveFile.Close()
		os.RemoveAll(checkout)
		return "", fmt.Errorf("factory: archiving previous build %s: %w", commit, err)
	}
	if err := archiveFile.Close(); err != nil {
		os.RemoveAll(checkout)
		return "", fmt.Errorf("factory: closing previous build archive: %w", err)
	}
	if err := unpackArchive(archivePath, checkout); err != nil {
		os.RemoveAll(checkout)
		return "", err
	}
	if err := os.Remove(archivePath); err != nil {
		os.RemoveAll(checkout)
		return "", fmt.Errorf("factory: removing previous build archive: %w", err)
	}
	return checkout, nil
}

func unpackArchive(archivePath, checkout string) error {
	archiveFile, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("factory: opening previous build archive: %w", err)
	}
	defer archiveFile.Close()
	reader := tar.NewReader(archiveFile)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("factory: reading previous build archive: %w", err)
		}
		name, err := archiveName(header.Name)
		if err != nil {
			return err
		}
		path := filepath.Join(checkout, name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return fmt.Errorf("factory: making archived directory %s: %w", name, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return fmt.Errorf("factory: making directory for archived file %s: %w", name, err)
			}
			file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				return fmt.Errorf("factory: making archived file %s: %w", name, err)
			}
			_, copyErr := io.Copy(file, reader)
			closeErr := file.Close()
			if copyErr != nil {
				return fmt.Errorf("factory: extracting archived file %s: %w", name, copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("factory: closing archived file %s: %w", name, closeErr)
			}
		}
	}
}

func archiveName(name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("factory: previous build archive contains unsafe path %q", name)
	}
	return clean, nil
}
