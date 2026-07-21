package analyze

import (
	"errors"
	"fmt"
	"os"

	"github.com/onixhdz/cartograph/internal/remote"
	"github.com/onixhdz/cartograph/internal/storage"
)

// TargetKind identifies how an analysis target should be acquired.
type TargetKind string

const (
	TargetLocal   TargetKind = "local"
	TargetRemote  TargetKind = "remote"
	TargetUnknown TargetKind = "unknown"
)

// Target is the deterministic interpretation of an Analyze target string.
type Target struct {
	Kind  TargetKind
	Value string
	Ref   string
}

// ResolveTarget applies the target rules shared by the CLI and embedded API.
// Interactive bare-name repository search remains a CLI concern and is
// represented by TargetUnknown.
func ResolveTarget(input, explicitRef, dataDir string) (Target, error) {
	if input == "" {
		return Target{}, errors.New("target is required")
	}

	// Scheme and SSH Git URLs are unambiguous. Classify them before touching
	// the filesystem because some valid URLs are invalid paths on Windows.
	if remote.IsGitURL(input) {
		return Target{Kind: TargetRemote, Value: input, Ref: explicitRef}, nil
	}

	// Existing shorthand-like paths are authoritative, including names with
	// an @ suffix.
	if _, err := os.Stat(input); err == nil {
		if explicitRef != "" {
			return Target{}, fmt.Errorf("ref %q requires a remote target", explicitRef)
		}
		return Target{Kind: TargetLocal, Value: input}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Target{}, fmt.Errorf("inspect target %q: %w", input, err)
	}

	value := input
	ref := explicitRef
	if base, inlineRef := remote.SplitRef(input); inlineRef != "" {
		if explicitRef != "" {
			return Target{}, fmt.Errorf("cannot use inline ref %q with explicit ref %q", inlineRef, explicitRef)
		}
		value, ref = base, inlineRef
	}

	if dataDir == "" {
		dataDir = storage.DefaultDataDir()
	}
	registry, err := storage.NewRegistry(dataDir)
	if err != nil {
		return Target{}, fmt.Errorf("open analysis registry: %w", err)
	}

	// Try the complete input first so a stored name such as group/project@v2
	// wins over interpreting group/project as GitHub shorthand.
	entry, resolveErr := registry.Resolve(input)
	if resolveErr == nil {
		return resolveRegistryTarget(entry, ref)
	}
	if !errors.Is(resolveErr, storage.ErrRegistryRepoNotFound) {
		return Target{}, fmt.Errorf("resolve registry target %q: %w", input, resolveErr)
	}
	if value != input {
		entry, resolveErr = registry.Resolve(value)
		if resolveErr == nil {
			return resolveRegistryTarget(entry, ref)
		}
		if !errors.Is(resolveErr, storage.ErrRegistryRepoNotFound) {
			return Target{}, fmt.Errorf("resolve registry target %q: %w", value, resolveErr)
		}
	}

	if remote.IsGitHostURL(value) {
		return Target{Kind: TargetRemote, Value: remote.ExpandGitHostURL(value), Ref: ref}, nil
	}
	if remote.IsGitHubShorthand(value) {
		return Target{Kind: TargetRemote, Value: remote.ExpandGitHubShorthand(value), Ref: ref}, nil
	}
	if remote.IsBareProjectName(value) {
		return Target{Kind: TargetUnknown, Value: value, Ref: ref}, nil
	}
	if ref != "" {
		return Target{}, fmt.Errorf("ref %q requires a remote target", ref)
	}
	return Target{Kind: TargetLocal, Value: value}, nil
}

func resolveRegistryTarget(entry storage.RegistryEntry, ref string) (Target, error) {
	if entry.URL != "" {
		canonicalURL, storedRef := remote.SplitRef(entry.URL)
		cloneURL := entry.Path
		if cloneURL == "" {
			cloneURL = canonicalURL
			if remote.IsGitHostURL(cloneURL) {
				cloneURL = remote.ExpandGitHostURL(cloneURL)
			}
		}
		if ref == "" {
			ref = storedRef
		}
		return Target{Kind: TargetRemote, Value: cloneURL, Ref: ref}, nil
	}
	if entry.Meta.SourcePath != "" {
		if ref != "" {
			return Target{}, fmt.Errorf("ref %q requires a remote target", ref)
		}
		return Target{Kind: TargetLocal, Value: entry.Meta.SourcePath}, nil
	}
	return Target{}, fmt.Errorf("registry target %q has no source path or clone URL", entry.Name)
}
