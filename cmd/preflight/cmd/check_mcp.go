package cmd

import (
	"fmt"
	"path/filepath"
	rt "runtime"

	"github.com/redhat-openshift-ecosystem/openshift-preflight/artifacts"
	"github.com/redhat-openshift-ecosystem/openshift-preflight/internal/cli"
	"github.com/redhat-openshift-ecosystem/openshift-preflight/internal/formatters"
	"github.com/redhat-openshift-ecosystem/openshift-preflight/internal/lib"
	"github.com/redhat-openshift-ecosystem/openshift-preflight/internal/runtime"
	"github.com/redhat-openshift-ecosystem/openshift-preflight/internal/viper"
	"github.com/redhat-openshift-ecosystem/openshift-preflight/mcp"
	"github.com/redhat-openshift-ecosystem/openshift-preflight/version"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

func checkMCPCmd(runpreflight runPreflight) *cobra.Command {
	checkMCPCmd := &cobra.Command{
		Use:   "mcp",
		Short: "Runs checks for an mcp server",
		Long:  `This command wil run the Certification checks for an mcp server`,
		Args:  checkMCPPositionalArgs,
		// this fmt.Sprintf is in place to keep spacing consistent with cobras two spaces that's used in: Usage, Flags, etc
		Example: fmt.Sprintf("  %s", "preflight check mcp quay.io/repo-name/mcp-server-name:version"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return checkMCPRunE(cmd, args, runpreflight)
		},
	}

	flags := checkMCPCmd.Flags()

	viper := viper.Instance()

	flags.String("platform", rt.GOARCH, "Architecture of image to pull. Defaults to runtime platform.")
	_ = viper.BindPFlag("platform", flags.Lookup("platform"))

	return checkMCPCmd
}

func checkMCPRunE(cmd *cobra.Command, args []string, runpreflight runPreflight) error {
	ctx := cmd.Context()
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return fmt.Errorf("invalid logging configuration")
	}
	logger.Info("certification library version", "version", version.Version.String())

	mcpImage := args[0]

	// Render the Viper configuration as a runtime.Config
	cfg, err := runtime.NewConfigFrom(*viper.Instance())
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	cfg.Image = mcpImage

	containerImagePlatforms, err := platformsToBeProcessed(cmd, cfg)
	if err != nil {
		return err
	}

	for _, platform := range containerImagePlatforms {
		logger.Info(fmt.Sprintf("running checks for %s for platform %s", mcpImage, platform))
		artifactsWriter, err := artifacts.NewFilesystemWriter(artifacts.WithDirectory(filepath.Join(cfg.Artifacts, platform)))
		if err != nil {
			return err
		}

		// Add the artifact writer to the context for use by checks.
		ctx := artifacts.ContextWithWriter(ctx, artifactsWriter)

		formatter, err := formatters.NewByName(formatters.DefaultFormat)
		if err != nil {
			return err
		}

		opts := generateMCPCheckOptions(cfg)
		opts = append(opts, mcp.WithPlatform(platform))

		checkmcp := mcp.NewCheck(
			mcpImage,
			opts...,
		)

		// Run the  mcp check.
		cmd.SilenceUsage = true

		if err := runpreflight(
			ctx,
			checkmcp.Run,
			cli.CheckConfig{
				IncludeJUnitResults: cfg.WriteJUnit,
				SubmitResults:       cfg.Submit,
			},
			formatter,
			&runtime.ResultWriterFile{},
			lib.NewNoopSubmitter(true, nil), // todo: wire in a real submitter if need be
		); err != nil {
			return err
		}
	}

	return nil
}

func checkMCPPositionalArgs(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("a container image positional argument is required")
	}

	return nil
}

// generateMCPCheckOptions returns appropriate mcp.Options based on cfg.
func generateMCPCheckOptions(cfg *runtime.Config) []mcp.Option {
	o := []mcp.Option{
		mcp.WithDockerConfigJSONFromFile(cfg.DockerConfig),
		mcp.WithPlatform(cfg.Platform),
	}

	return o
}
