/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package version implements the `version` command
package version

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"sigs.k8s.io/kind/pkg/cmd"
	"sigs.k8s.io/kind/pkg/log"
)

// Version returns the kind CLI Semantic Version
func Version() string {
	var v string
	if gitTag != "" {
		v = gitTag
	} else {
		v = packageVersion
		if gitCommit != "" {
			// NOTE: use 14 character short hash, like Kubernetes
			v += " GitCommit:" + truncate(gitCommit, 14)
		}
	}
	return v
}

// DisplayVersion is Version() display formatted, this is what the version
// subcommand prints
func DisplayVersion() string {
	return "cloud-provisioner Version:" + Version() + " GoVersion:" + runtime.Version() + " Platform:" + runtime.GOOS + "/" + runtime.GOARCH
}

// gitTag is the git tag used to build the kind binary, if available.
// It is injected at build time.
var gitTag = ""

// gitCommitCount count the commits since the last release.
// It is injected at build time.
var gitCommitCount = ""

// gitCommit is the commit used to build the kind binary, if available.
// It is injected at build time.
var gitCommit = ""

// packageVersion is the name used to name the artifact, if available.
// It is injected at build time.
var packageVersion = ""

// NewCommand returns a new cobra.Command for version
func NewCommand(logger log.Logger, streams cmd.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "version",
		Short: "Prints the cloud-provisioner CLI version",
		Long:  "Prints the cloud-provisioner CLI version",
		RunE: func(cmd *cobra.Command, args []string) error {
			if logger.V(0).Enabled() {
				// if not -q / --quiet, show lots of info
				fmt.Fprintln(streams.Out, DisplayVersion())
			} else {
				// otherwise only show semver
				fmt.Fprintln(streams.Out, Version())
			}
			return nil
		},
	}
	return cmd
}

func truncate(s string, maxLen int) string {
	if len(s) < maxLen {
		return s
	}
	return s[:maxLen]
}
