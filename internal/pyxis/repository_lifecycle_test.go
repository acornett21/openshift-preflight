package pyxis

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	cranev1 "github.com/google/go-containerregistry/pkg/v1"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
)

var _ = Describe("RepositoryLifecycleForLayers", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("deduplicates DiffIDs after filtering excluded layers", func() {
		excluded, err := cranev1.NewHash("sha256:5f70bf18a086007016e948b04aed3b82103a36bea41755b6cddfaf10ace3c6ef")
		Expect(err).ToNot(HaveOccurred())
		layer, err := cranev1.NewHash("sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
		Expect(err).ToNot(HaveOccurred())

		layers := deduplicateLifecycleLayers(filterExcludedLayers([]cranev1.Hash{layer, excluded, layer}))

		Expect(layers).To(Equal([]string{layer.String()}))
	})

	It("paginates in stable order and returns lifecycle metadata for all aliases", func() {
		firstLayer := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
		secondLayer := "sha256:2222222222222222222222222222222222222222222222222222222222222222"
		pages := make([]int, 0, 2)
		var queries []string
		var registries [][]string
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			Expect(err).ToNot(HaveOccurred())
			Expect(r.Body.Close()).To(Succeed())

			var request struct {
				Query     string `json:"query"`
				Variables struct {
					CurrentPage int      `json:"currentPage"`
					Registries  []string `json:"registries"`
				} `json:"variables"`
			}
			Expect(json.Unmarshal(body, &request)).To(Succeed())
			pages = append(pages, request.Variables.CurrentPage)
			queries = append(queries, request.Query)
			registries = append(registries, request.Variables.Registries)

			if request.Variables.CurrentPage == 0 {
				writeRepositoryLifecycleResponse(w, 2, 0, repositoryLifecycleImage(firstLayer, "image-1", "ubi9/ubi", nil))
				return
			}

			writeRepositoryLifecycleResponse(w, 2, 1, repositoryLifecycleImage(secondLayer, "image-2", "ubi9/ubi-minimal", nil))
		})
		client := NewPyxisClient("my.pyxis.host/query/", "", "", &http.Client{Transport: localRoundTripper{handler: handler}})

		first, err := cranev1.NewHash(firstLayer)
		Expect(err).ToNot(HaveOccurred())
		second, err := cranev1.NewHash(secondLayer)
		Expect(err).ToNot(HaveOccurred())
		lifecycles, err := client.RepositoryLifecycleForLayers(ctx, []cranev1.Hash{first, second})

		Expect(err).ToNot(HaveOccurred())
		Expect(pages).To(Equal([]int{0, 1}))
		Expect(lifecycles).To(HaveLen(2))
		Expect(lifecycles[0].DiffID).To(Equal(firstLayer))
		Expect(lifecycles[0].EOLDate).To(BeNil())
		Expect(lifecycles[0].ReleaseCategories).To(Equal([]string{"Generally Available"}))
		Expect(lifecycles[0].MetadataSource).To(Equal("quay"))
		Expect(registries).To(Equal([][]string{{RepositoryLifecycleRegistry}, {RepositoryLifecycleRegistry}}))
		Expect(queries[0]).To(ContainSubstring("repositories"))
		Expect(queries[0]).To(ContainSubstring("uncompressed_top_layer_id"))
		Expect(queries[0]).To(ContainSubstring("sort_by"))
		Expect(queries[0]).ToNot(ContainSubstring("certified"))
	})

	It("returns no data without making a query when all layers are excluded", func() {
		called := false
		handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			called = true
		})
		client := NewPyxisClient("my.pyxis.host/query/", "", "", &http.Client{Transport: localRoundTripper{handler: handler}})
		excluded, err := cranev1.NewHash("sha256:5f70bf18a086007016e948b04aed3b82103a36bea41755b6cddfaf10ace3c6ef")
		Expect(err).ToNot(HaveOccurred())

		lifecycles, err := client.RepositoryLifecycleForLayers(ctx, []cranev1.Hash{excluded})

		Expect(err).ToNot(HaveOccurred())
		Expect(lifecycles).To(BeEmpty())
		Expect(called).To(BeFalse())
	})

	ginkgo.DescribeTable("rejects invalid response data", func(response string, expectedMessage string) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, err := w.Write([]byte(response))
			Expect(err).ToNot(HaveOccurred())
		})
		client := NewPyxisClient("my.pyxis.host/query/", "", "", &http.Client{Transport: localRoundTripper{handler: handler}})
		layer, err := cranev1.NewHash("sha256:3333333333333333333333333333333333333333333333333333333333333333")
		Expect(err).ToNot(HaveOccurred())

		lifecycles, err := client.RepositoryLifecycleForLayers(ctx, []cranev1.Hash{layer})

		Expect(lifecycles).To(BeNil())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(expectedMessage))
	},
		ginkgo.Entry("outer GraphQL error", `{"errors":[{"message":"graphql unavailable"}]}`, "graphql unavailable"),
		ginkgo.Entry("null image data", `{"data":{"find_images":{"error":null,"total":0,"page":0,"data":null}}}`, "null image data"),
		ginkgo.Entry("nested repository error", `{"data":{"find_images":{"error":null,"total":1,"page":0,"data":[{"_id":"image-1","uncompressed_top_layer_id":"sha256:3333333333333333333333333333333333333333333333333333333333333333","repositories":[{"registry":"registry.access.redhat.com","repository":"ubi9/ubi","edges":{"repository":{"error":{"status":500,"detail":"repository unavailable"},"data":null}}}]}]}}}`, "repository unavailable"),
		ginkgo.Entry("null nested data", `{"data":{"find_images":{"error":null,"total":1,"page":0,"data":[{"_id":"image-1","uncompressed_top_layer_id":"sha256:3333333333333333333333333333333333333333333333333333333333333333","repositories":[{"registry":"registry.access.redhat.com","repository":"ubi9/ubi","edges":{"repository":{"error":null,"data":null}}}]}]}}}`, "null lifecycle data"),
		ginkgo.Entry("malformed EOL date", `{"data":{"find_images":{"error":null,"total":1,"page":0,"data":[{"_id":"image-1","uncompressed_top_layer_id":"sha256:3333333333333333333333333333333333333333333333333333333333333333","repositories":[{"registry":"registry.access.redhat.com","repository":"ubi9/ubi","edges":{"repository":{"error":null,"data":{"_id":"repo-1","registry":"registry.access.redhat.com","repository":"ubi9/ubi","eol_date":"not-a-date","release_categories":["Deprecated"],"metadata_source":"quay"}}}}]}]}}}`, "invalid RFC3339"),
	)

	It("rejects a conflicting nested repository identity", func() {
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeRepositoryLifecycleResponse(w, 1, 0, repositoryLifecycleImage(
				"sha256:4444444444444444444444444444444444444444444444444444444444444444",
				"image-1",
				"ubi9/ubi",
				map[string]any{"repository": "ubi9/ubi-minimal"},
			))
		})
		client := NewPyxisClient("my.pyxis.host/query/", "", "", &http.Client{Transport: localRoundTripper{handler: handler}})
		layer, err := cranev1.NewHash("sha256:4444444444444444444444444444444444444444444444444444444444444444")
		Expect(err).ToNot(HaveOccurred())

		_, err = client.RepositoryLifecycleForLayers(ctx, []cranev1.Hash{layer})

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("conflicting repository values"))
	})
})

func writeRepositoryLifecycleResponse(w http.ResponseWriter, total int, page int, images ...map[string]any) {
	response := map[string]any{
		"data": map[string]any{
			"find_images": map[string]any{
				"error": nil,
				"total": total,
				"page":  page,
				"data":  images,
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	Expect(json.NewEncoder(w).Encode(response)).To(Succeed())
}

func repositoryLifecycleImage(diffID string, imageID string, repository string, overrides map[string]any) map[string]any {
	repositoryData := map[string]any{
		"_id":                "repository-1",
		"registry":           RepositoryLifecycleRegistry,
		"repository":         repository,
		"eol_date":           nil,
		"release_categories": []string{"Generally Available"},
		"metadata_source":    "quay",
	}
	for key, value := range overrides {
		repositoryData[key] = value
	}

	return map[string]any{
		"_id":                       imageID,
		"uncompressed_top_layer_id": diffID,
		"repositories": []any{
			map[string]any{
				"registry":   RepositoryLifecycleRegistry,
				"repository": repository,
				"edges": map[string]any{
					"repository": map[string]any{
						"error": nil,
						"data":  repositoryData,
					},
				},
			},
		},
	}
}
