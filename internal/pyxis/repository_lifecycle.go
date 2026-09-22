package pyxis

import (
	"context"
	"fmt"
	"net/http"
	"time"

	cranev1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/shurcooL/graphql"
)

const (
	repositoryLifecyclePageSize = 100
)

// RepositoryLifecycleRegistry is the Red Hat registry used for lifecycle
// lookups.
const RepositoryLifecycleRegistry = "registry.access.redhat.com"

// RepositoryLifecycle contains lifecycle metadata for a Red Hat repository
// associated with an image layer.
type RepositoryLifecycle struct {
	ImageID           string
	DiffID            string
	Registry          string
	Repository        string
	EOLDate           *time.Time
	ReleaseCategories []string
	MetadataSource    string
}

// RepositoryLifecycleForLayers returns lifecycle metadata for Red Hat
// repositories whose images contain one of the supplied uncompressed layer
// DiffIDs.
func (p *pyxisClient) RepositoryLifecycleForLayers(ctx context.Context, uncompressedLayerHashes []cranev1.Hash) ([]RepositoryLifecycle, error) {
	filteredHashes := deduplicateLifecycleLayers(filterExcludedLayers(uncompressedLayerHashes))
	if len(filteredHashes) == 0 {
		return []RepositoryLifecycle{}, nil
	}

	httpClient, ok := p.Client.(*http.Client)
	if !ok {
		//coverage:ignore
		return nil, fmt.Errorf("client could not be used as http.Client")
	}

	layerIDs := make([]graphql.String, len(filteredHashes))
	for i, layer := range filteredHashes {
		layerIDs[i] = graphql.String(layer)
	}

	client := graphql.NewClient(p.getPyxisGraphqlURL(), httpClient)
	variables := map[string]any{
		"contImageLayers": layerIDs,
		"currentPage":     graphql.Int(0),
		"pageSize":        graphql.Int(repositoryLifecyclePageSize),
		"registries":      []graphql.String{RepositoryLifecycleRegistry},
	}

	lifecycles := make([]RepositoryLifecycle, 0)
	fetchedImages := 0
	for page := 0; ; page++ {
		variables["currentPage"] = graphql.Int(page)

		// Keep the GraphQL shape inline so it can be copied into the Pyxis playground.
		var query struct {
			FindImages struct {
				Error *struct {
					Status graphql.Int    `graphql:"status"`
					Detail graphql.String `graphql:"detail"`
				} `graphql:"error"`
				Total graphql.Int
				Page  graphql.Int
				Data  []struct {
					ID                     graphql.String `graphql:"_id"`
					UncompressedTopLayerID graphql.String `graphql:"uncompressed_top_layer_id"`
					Repositories           []struct {
						Registry   graphql.String
						Repository graphql.String
						Edges      *struct {
							Repository *struct {
								Error *struct {
									Status graphql.Int    `graphql:"status"`
									Detail graphql.String `graphql:"detail"`
								} `graphql:"error"`
								Data *struct {
									ID                graphql.String   `graphql:"_id"`
									Registry          graphql.String   `graphql:"registry"`
									Repository        graphql.String   `graphql:"repository"`
									EOLDate           *graphql.String  `graphql:"eol_date"`
									ReleaseCategories []graphql.String `graphql:"release_categories"`
									MetadataSource    graphql.String   `graphql:"metadata_source"`
								} `graphql:"data"`
							} `graphql:"repository"`
						} `graphql:"edges"`
					} `graphql:"repositories"`
				} `graphql:"data"`
			} `graphql:"find_images(page:$currentPage,page_size:$pageSize,sort_by:[{field:\"_id\",order:ASC}],filter:{and:[{repositories:{registry:{in:$registries}}}{uncompressed_top_layer_id:{in:$contImageLayers}}]})"`
		}
		if err := client.Query(ctx, &query, variables); err != nil {
			return nil, fmt.Errorf("error while executing repository lifecycle query: %w", err)
		}

		if query.FindImages.Error != nil {
			return nil, fmt.Errorf("repository lifecycle query returned an error: status %d: %s", query.FindImages.Error.Status, query.FindImages.Error.Detail)
		}
		if query.FindImages.Data == nil {
			return nil, fmt.Errorf("repository lifecycle query returned null image data")
		}

		images := query.FindImages.Data
		if len(images) == 0 {
			if int(query.FindImages.Total) > fetchedImages {
				return nil, fmt.Errorf("repository lifecycle query returned incomplete pagination data")
			}
			return lifecycles, nil
		}

		fetchedImages += len(images)
		for _, image := range images {
			imageID := string(image.ID)
			diffID := string(image.UncompressedTopLayerID)
			if imageID == "" || diffID == "" {
				return nil, fmt.Errorf("repository lifecycle query returned image data without an image ID or DiffID")
			}
			if image.Repositories == nil {
				return nil, fmt.Errorf("repository lifecycle query returned null repository data for image %s", imageID)
			}

			matchedRegistry := false
			for _, imageRepository := range image.Repositories {
				registry := string(imageRepository.Registry)
				if registry != RepositoryLifecycleRegistry {
					continue
				}
				matchedRegistry = true

				if imageRepository.Edges == nil || imageRepository.Edges.Repository == nil {
					return nil, fmt.Errorf("repository lifecycle query returned null repository data for image %s and repository %s", imageID, imageRepository.Repository)
				}

				repository := imageRepository.Edges.Repository
				if repository.Error != nil {
					return nil, fmt.Errorf("repository lifecycle query returned an error for image %s and repository %s: status %d: %s", imageID, imageRepository.Repository, repository.Error.Status, repository.Error.Detail)
				}
				if repository.Data == nil {
					return nil, fmt.Errorf("repository lifecycle query returned null lifecycle data for image %s and repository %s", imageID, imageRepository.Repository)
				}

				lifecycle, err := repositoryLifecycleFromQuery(
					imageID,
					diffID,
					registry,
					string(imageRepository.Repository),
					string(repository.Data.Registry),
					string(repository.Data.Repository),
					repository.Data.EOLDate,
					repository.Data.ReleaseCategories,
					string(repository.Data.MetadataSource),
				)
				if err != nil {
					return nil, err
				}
				lifecycles = append(lifecycles, lifecycle)
			}

			if !matchedRegistry {
				return nil, fmt.Errorf("repository lifecycle query returned no %s repository alias for image %s", RepositoryLifecycleRegistry, imageID)
			}
		}

		if int(query.FindImages.Total) == 0 || fetchedImages >= int(query.FindImages.Total) {
			return lifecycles, nil
		}
	}
}

func deduplicateLifecycleLayers(layers []cranev1.Hash) []string {
	unique := make([]string, 0, len(layers))
	seen := make(map[string]struct{}, len(layers))
	for _, layer := range layers {
		layerID := layer.String()
		if _, ok := seen[layerID]; ok {
			continue
		}

		seen[layerID] = struct{}{}
		unique = append(unique, layerID)
	}

	return unique
}

func repositoryLifecycleFromQuery(
	imageID string,
	diffID string,
	registry string,
	repository string,
	dataRegistry string,
	dataRepository string,
	eolDateValue *graphql.String,
	dataReleaseCategories []graphql.String,
	metadataSource string,
) (RepositoryLifecycle, error) {
	if dataRegistry != "" && dataRegistry != registry {
		return RepositoryLifecycle{}, fmt.Errorf("repository lifecycle query returned conflicting registry values for image %s and repository %s", imageID, repository)
	}
	if dataRepository != "" && dataRepository != repository {
		return RepositoryLifecycle{}, fmt.Errorf("repository lifecycle query returned conflicting repository values for image %s and repository %s", imageID, repository)
	}

	var eolDate *time.Time
	if eolDateValue != nil {
		parsed, err := time.Parse(time.RFC3339, string(*eolDateValue))
		if err != nil {
			return RepositoryLifecycle{}, fmt.Errorf("invalid RFC3339 eol_date %q for image %s and repository %s: %w", *eolDateValue, imageID, repository, err)
		}
		eolDate = &parsed
	}

	releaseCategories := make([]string, len(dataReleaseCategories))
	for i, category := range dataReleaseCategories {
		releaseCategories[i] = string(category)
	}

	return RepositoryLifecycle{
		ImageID:           imageID,
		DiffID:            diffID,
		Registry:          registry,
		Repository:        repository,
		EOLDate:           eolDate,
		ReleaseCategories: releaseCategories,
		MetadataSource:    metadataSource,
	}, nil
}
