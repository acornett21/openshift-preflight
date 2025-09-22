package mcp

import (
	"context"
	"fmt"

	"github.com/redhat-openshift-ecosystem/openshift-preflight/certification"
	preflighterr "github.com/redhat-openshift-ecosystem/openshift-preflight/errors"
	"github.com/redhat-openshift-ecosystem/openshift-preflight/internal/check"
	"github.com/redhat-openshift-ecosystem/openshift-preflight/internal/engine"
	"github.com/redhat-openshift-ecosystem/openshift-preflight/internal/policy"
	"github.com/redhat-openshift-ecosystem/openshift-preflight/internal/runtime"
)

type Option = func(*mcpCheck)

func NewCheck(image string, opts ...Option) *mcpCheck {
	m := &mcpCheck{
		image: image,
	}
	for _, opt := range opts {
		opt(m)
	}

	return m
}

func (m *mcpCheck) Run(ctx context.Context) (certification.Results, error) {
	err := m.resolve(ctx)
	if err != nil {
		return certification.Results{}, err
	}

	cfg := runtime.Config{
		Image:        m.image,
		DockerConfig: m.dockerconfigjson,
		Scratch:      false,
		Bundle:       false,
	}
	eng, err := engine.New(ctx, m.checks, nil, cfg)
	if err != nil {
		return certification.Results{}, err
	}

	if err := eng.ExecuteChecks(ctx); err != nil {
		return certification.Results{}, err
	}

	return eng.Results(ctx), nil
}

func (m *mcpCheck) resolve(ctx context.Context) error {
	if m.resolved {
		return nil
	}

	if m.image == "" {
		return preflighterr.ErrImageEmpty
	}

	m.policy = policy.PolicyMCP

	newChecks, err := engine.InitializeMCPChecks(ctx, m.policy, engine.CommonCheckConfig{
		DockerConfig: m.dockerconfigjson,
	})
	if err != nil {
		return fmt.Errorf("%w: %s", preflighterr.ErrCannotInitializeChecks, err)
	}

	m.checks = newChecks
	m.resolved = true

	return nil
}

func (m *mcpCheck) List(ctx context.Context) (policy.Policy, []check.Check, error) {
	return m.policy, m.checks, m.resolve(ctx)
}

func WithDockerConfigJSONFromFile(s string) Option {
	return func(mm *mcpCheck) {
		mm.dockerconfigjson = s
	}
}

// WithPlatform will define for what platform the image should be pulled.
// E.g. amd64, s390x.
func WithPlatform(platform string) Option {
	return func(mm *mcpCheck) {
		mm.platform = platform
	}
}

type mcpCheck struct {
	image            string
	dockerconfigjson string
	platform         string
	resolved         bool
	checks           []check.Check
	policy           policy.Policy
}
