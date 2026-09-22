package container

import (
	"context"
	"fmt"
	"strings"
	"time"

	cranev1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/redhat-openshift-ecosystem/openshift-preflight/internal/check"
	"github.com/redhat-openshift-ecosystem/openshift-preflight/internal/image"
	"github.com/redhat-openshift-ecosystem/openshift-preflight/internal/pyxis"
)

var _ check.Check = &HasSupportedRedHatBaseImageCheck{}

// HasSupportedRedHatBaseImageCheck evaluates whether the Red Hat repository
// containing the image's base layer is still supported and not Deprecated.
type HasSupportedRedHatBaseImageCheck struct {
	RepositoryLifecycleFinder repositoryLifecycleFinder
	now                       func() time.Time
}

type repositoryLifecycleFinder interface {
	RepositoryLifecycleForLayers(context.Context, []cranev1.Hash) ([]pyxis.RepositoryLifecycle, error)
}

// NewHasSupportedRedHatBaseImageCheck creates a warning-level base image
// lifecycle check.
func NewHasSupportedRedHatBaseImageCheck(finder repositoryLifecycleFinder, now func() time.Time) *HasSupportedRedHatBaseImageCheck {
	if now == nil {
		now = time.Now
	}

	return &HasSupportedRedHatBaseImageCheck{
		RepositoryLifecycleFinder: finder,
		now:                       now,
	}
}

func (p *HasSupportedRedHatBaseImageCheck) Validate(ctx context.Context, imgRef image.ImageReference) (bool, error) {
	layerHashes, err := getImageLayers(imgRef.ImageInfo)
	if err != nil {
		return false, fmt.Errorf("could not get image layers: %w", err)
	}

	return p.validate(ctx, layerHashes)
}

func (p *HasSupportedRedHatBaseImageCheck) validate(ctx context.Context, layerHashes []cranev1.Hash) (bool, error) {
	if p.RepositoryLifecycleFinder == nil {
		return false, fmt.Errorf("repository lifecycle finder is required")
	}

	lifecycles, err := p.RepositoryLifecycleFinder.RepositoryLifecycleForLayers(ctx, layerHashes)
	if err != nil {
		return false, fmt.Errorf("repository lifecycle query failed: %w", err)
	}

	selectedDiffID, err := selectHighestMatchingDiffID(layerHashes, lifecycles)
	if err != nil {
		return false, err
	}

	selected := make([]pyxis.RepositoryLifecycle, 0, len(lifecycles))
	for _, lifecycle := range lifecycles {
		if lifecycle.DiffID == selectedDiffID {
			selected = append(selected, lifecycle)
		}
	}
	if len(selected) == 0 {
		return false, fmt.Errorf("no repository lifecycle data matched DiffID %s", selectedDiffID)
	}

	return classifyRepositoryLifecycles(selected, p.currentTime())
}

func (p *HasSupportedRedHatBaseImageCheck) currentTime() time.Time {
	if p.now == nil {
		return time.Now()
	}

	return p.now()
}

func selectHighestMatchingDiffID(diffIDs []cranev1.Hash, lifecycles []pyxis.RepositoryLifecycle) (string, error) {
	positions := make(map[string]int, len(diffIDs))
	for i, diffID := range diffIDs {
		positions[diffID.String()] = i
	}

	selectedPosition := -1
	selectedDiffID := ""
	for _, lifecycle := range lifecycles {
		position, ok := positions[lifecycle.DiffID]
		if ok && position > selectedPosition {
			selectedPosition = position
			selectedDiffID = lifecycle.DiffID
		}
	}
	if selectedPosition == -1 {
		return "", fmt.Errorf("no repository lifecycle data matched any image DiffID")
	}

	return selectedDiffID, nil
}

func classifyRepositoryLifecycles(lifecycles []pyxis.RepositoryLifecycle, now time.Time) (bool, error) {
	var eol *bool
	for _, lifecycle := range lifecycles {
		if err := validateRepositoryLifecycle(lifecycle); err != nil {
			return false, err
		}

		isEOL := lifecycle.EOLDate != nil && !lifecycle.EOLDate.After(now)
		if eol == nil {
			eol = &isEOL
			continue
		}
		if *eol != isEOL {
			return false, fmt.Errorf("conflicting lifecycle status for DiffID %s", lifecycle.DiffID)
		}
	}

	if eol == nil {
		return false, fmt.Errorf("no repository lifecycle data was provided")
	}
	if *eol {
		return false, nil
	}

	return true, nil
}

func validateRepositoryLifecycle(lifecycle pyxis.RepositoryLifecycle) error {
	if strings.TrimSpace(lifecycle.ImageID) == "" {
		return fmt.Errorf("repository lifecycle data is missing an image ID")
	}
	if strings.TrimSpace(lifecycle.DiffID) == "" {
		return fmt.Errorf("repository lifecycle data is missing a DiffID")
	}
	if lifecycle.Registry != pyxis.RepositoryLifecycleRegistry {
		return fmt.Errorf("repository lifecycle data has unsupported registry %q", lifecycle.Registry)
	}
	if strings.TrimSpace(lifecycle.Repository) == "" {
		return fmt.Errorf("repository lifecycle data is missing a repository")
	}
	if len(lifecycle.ReleaseCategories) == 0 {
		return fmt.Errorf("repository lifecycle data is missing release categories for repository %s", lifecycle.Repository)
	}
	for _, category := range lifecycle.ReleaseCategories {
		if strings.TrimSpace(category) == "" {
			return fmt.Errorf("repository lifecycle data has an empty release category for repository %s", lifecycle.Repository)
		}
	}
	return nil
}

func (p *HasSupportedRedHatBaseImageCheck) Name() string {
	return "HasSupportedRedHatBaseImage"
}

func (p *HasSupportedRedHatBaseImageCheck) Metadata() check.Metadata {
	return check.Metadata{
		Description:      "Checking if the container's Red Hat base image repository is supported",
		Level:            check.LevelWarn,
		KnowledgeBaseURL: certDocumentationURL,
		CheckURL:         certDocumentationURL,
	}
}

func (p *HasSupportedRedHatBaseImageCheck) Help() check.HelpText {
	return check.HelpText{
		Message:    "Check HasSupportedRedHatBaseImage encountered an error. Please review the preflight.log file for more information.",
		Suggestion: "Use a supported (non-Deprecated) Red Hat base image from https://catalog.redhat.com/software/containers/",
	}
}

func (p *HasSupportedRedHatBaseImageCheck) RequiredFilePatterns() []string {
	return nil
}
