package container

import (
	"context"
	"errors"
	"time"

	cranev1 "github.com/google/go-containerregistry/pkg/v1"
	fakecranev1 "github.com/google/go-containerregistry/pkg/v1/fake"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/redhat-openshift-ecosystem/openshift-preflight/internal/check"
	"github.com/redhat-openshift-ecosystem/openshift-preflight/internal/image"
	"github.com/redhat-openshift-ecosystem/openshift-preflight/internal/pyxis"
)

type fakeRepositoryLifecycleFinder struct {
	lifecycleData []pyxis.RepositoryLifecycle
	err           error
}

func (f *fakeRepositoryLifecycleFinder) RepositoryLifecycleForLayers(context.Context, []cranev1.Hash) ([]pyxis.RepositoryLifecycle, error) {
	return f.lifecycleData, f.err
}

var _ = Describe("HasSupportedRedHatBaseImage", func() {
	now := time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)
	lowerLayer := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	topLayer := "sha256:2222222222222222222222222222222222222222222222222222222222222222"

	It("passes for supported lifecycle data with a null EOL date", func() {
		lifecycleCheck := newLifecycleCheck(now, lifecycleRecord(topLayer, nil, "ubi9/ubi"))

		passed, err := lifecycleCheck.Validate(context.Background(), imageReferenceWithDiffIDs(topLayer))

		Expect(err).ToNot(HaveOccurred())
		Expect(passed).To(BeTrue())
	})

	It("passes for GA lifecycle data without a metadata source", func() {
		lifecycle := lifecycleRecord(topLayer, nil, "ubi10/ubi-minimal")
		lifecycle.MetadataSource = ""
		lifecycleCheck := newLifecycleCheck(now, lifecycle)

		passed, err := lifecycleCheck.Validate(context.Background(), imageReferenceWithDiffIDs(topLayer))

		Expect(err).ToNot(HaveOccurred())
		Expect(passed).To(BeTrue())
	})

	DescribeTable("classifies EOL dates at the boundary", func(eolDate time.Time, expected bool) {
		lifecycleCheck := NewHasSupportedRedHatBaseImageCheck(
			&fakeRepositoryLifecycleFinder{
				lifecycleData: []pyxis.RepositoryLifecycle{lifecycleRecord(topLayer, &eolDate, "ubi9/ubi")},
			},
			func() time.Time { return now },
		)

		passed, err := lifecycleCheck.Validate(context.Background(), imageReferenceWithDiffIDs(topLayer))

		Expect(err).ToNot(HaveOccurred())
		Expect(passed).To(Equal(expected))
	},
		Entry("before EOL", now.Add(time.Second), true),
		Entry("at EOL", now, false),
		Entry("after EOL", now.Add(-time.Second), false),
	)

	It("selects the highest matching input DiffID", func() {
		eolDate := now.Add(-time.Hour)
		futureDate := now.Add(time.Hour)
		finder := &fakeRepositoryLifecycleFinder{
			lifecycleData: []pyxis.RepositoryLifecycle{
				lifecycleRecord(lowerLayer, &eolDate, "ubi8/ubi"),
				lifecycleRecord(topLayer, &futureDate, "ubi9/ubi"),
			},
		}
		lifecycleCheck := NewHasSupportedRedHatBaseImageCheck(finder, func() time.Time { return now })

		passed, err := lifecycleCheck.Validate(context.Background(), imageReferenceWithDiffIDs(lowerLayer, topLayer))

		Expect(err).ToNot(HaveOccurred())
		Expect(passed).To(BeTrue())
	})

	It("classifies all aliases for the selected layer", func() {
		finder := &fakeRepositoryLifecycleFinder{
			lifecycleData: []pyxis.RepositoryLifecycle{
				lifecycleRecord(topLayer, nil, "ubi9/ubi"),
				lifecycleRecord(topLayer, nil, "ubi9/ubi-minimal"),
			},
		}
		lifecycleCheck := NewHasSupportedRedHatBaseImageCheck(finder, func() time.Time { return now })

		passed, err := lifecycleCheck.Validate(context.Background(), imageReferenceWithDiffIDs(topLayer))

		Expect(err).ToNot(HaveOccurred())
		Expect(passed).To(BeTrue())
	})

	It("returns a non-enforcing failure for confirmed EOL aliases", func() {
		eolDate := now
		finder := &fakeRepositoryLifecycleFinder{
			lifecycleData: []pyxis.RepositoryLifecycle{
				lifecycleRecord(topLayer, &eolDate, "ubi9/ubi"),
				lifecycleRecord(topLayer, &eolDate, "ubi9/ubi-minimal"),
			},
		}
		lifecycleCheck := NewHasSupportedRedHatBaseImageCheck(finder, func() time.Time { return now })

		passed, err := lifecycleCheck.Validate(context.Background(), imageReferenceWithDiffIDs(topLayer))

		Expect(err).ToNot(HaveOccurred())
		Expect(passed).To(BeFalse())
	})

	It("returns an error for conflicting alias lifecycle status", func() {
		eolDate := now
		finder := &fakeRepositoryLifecycleFinder{
			lifecycleData: []pyxis.RepositoryLifecycle{
				lifecycleRecord(topLayer, nil, "ubi9/ubi"),
				lifecycleRecord(topLayer, &eolDate, "ubi9/ubi-minimal"),
			},
		}
		lifecycleCheck := NewHasSupportedRedHatBaseImageCheck(finder, func() time.Time { return now })

		passed, err := lifecycleCheck.Validate(context.Background(), imageReferenceWithDiffIDs(topLayer))

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("conflicting lifecycle status"))
		Expect(passed).To(BeFalse())
	})

	DescribeTable("returns an error for missing lifecycle data", func(records []pyxis.RepositoryLifecycle, expectedMessage string) {
		finder := &fakeRepositoryLifecycleFinder{lifecycleData: records}
		lifecycleCheck := NewHasSupportedRedHatBaseImageCheck(finder, func() time.Time { return now })

		passed, err := lifecycleCheck.Validate(context.Background(), imageReferenceWithDiffIDs(topLayer))

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(expectedMessage))
		Expect(passed).To(BeFalse())
	},
		Entry("no matching record", []pyxis.RepositoryLifecycle{}, "no repository lifecycle data matched"),
		Entry("missing image ID", []pyxis.RepositoryLifecycle{{DiffID: topLayer}}, "missing an image ID"),
		Entry("missing release categories", []pyxis.RepositoryLifecycle{{ImageID: "image-1", DiffID: topLayer, Registry: pyxis.RepositoryLifecycleRegistry, Repository: "ubi9/ubi", MetadataSource: "quay"}}, "missing release categories"),
	)

	It("returns an error when the Pyxis query fails", func() {
		finder := &fakeRepositoryLifecycleFinder{err: errors.New("Pyxis is unavailable")}
		lifecycleCheck := NewHasSupportedRedHatBaseImageCheck(finder, func() time.Time { return now })

		passed, err := lifecycleCheck.Validate(context.Background(), imageReferenceWithDiffIDs(topLayer))

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Pyxis is unavailable"))
		Expect(passed).To(BeFalse())
	})

	It("has warning metadata", func() {
		lifecycleCheck := NewHasSupportedRedHatBaseImageCheck(nil, time.Now)

		Expect(lifecycleCheck.Name()).To(Equal("HasSupportedRedHatBaseImage"))
		Expect(lifecycleCheck.Metadata().Level).To(Equal(check.LevelWarn))
		Expect(lifecycleCheck.RequiredFilePatterns()).To(BeNil())
	})

	AssertMetaData(NewHasSupportedRedHatBaseImageCheck(nil, time.Now))
})

func newLifecycleCheck(now time.Time, lifecycle pyxis.RepositoryLifecycle) *HasSupportedRedHatBaseImageCheck {
	return NewHasSupportedRedHatBaseImageCheck(
		&fakeRepositoryLifecycleFinder{lifecycleData: []pyxis.RepositoryLifecycle{lifecycle}},
		func() time.Time { return now },
	)
}

func lifecycleRecord(diffID string, eolDate *time.Time, repository string) pyxis.RepositoryLifecycle {
	return pyxis.RepositoryLifecycle{
		ImageID:           "image-1",
		DiffID:            diffID,
		Registry:          pyxis.RepositoryLifecycleRegistry,
		Repository:        repository,
		EOLDate:           eolDate,
		ReleaseCategories: []string{"Generally Available"},
		MetadataSource:    "quay",
	}
}

func imageReferenceWithDiffIDs(diffIDs ...string) image.ImageReference {
	hashes := make([]cranev1.Hash, len(diffIDs))
	for i, diffID := range diffIDs {
		hash, err := cranev1.NewHash(diffID)
		if err != nil {
			panic(err)
		}
		hashes[i] = hash
	}

	fakeImage := &fakecranev1.FakeImage{
		ConfigFileStub: func() (*cranev1.ConfigFile, error) {
			return &cranev1.ConfigFile{RootFS: cranev1.RootFS{DiffIDs: hashes}}, nil
		},
	}

	return image.ImageReference{ImageInfo: fakeImage}
}
