/*
 * Copyright 2018-2020 the original author or authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package libbs

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildpacks/libcnb"
	"github.com/paketo-buildpacks/libpak"
	"github.com/paketo-buildpacks/libpak/bard"
	"github.com/paketo-buildpacks/libpak/crush"
	"github.com/paketo-buildpacks/libpak/effect"
	"github.com/paketo-buildpacks/libpak/sherpa"
)

type Application struct {
	ApplicationPath  string
	Arguments        []string
	ArtifactResolver ArtifactResolver
	Cache            Cache
	Command          string
	Executor         effect.Executor
	LayerContributor libpak.LayerContributor
	Logger           bard.Logger
	Plan             *libcnb.BuildpackPlan
}

func (a Application) Contribute(layer libcnb.Layer) (libcnb.Layer, error) {
	a.LayerContributor.Logger = a.Logger

	layer, err := a.LayerContributor.Contribute(layer, func() (libcnb.Layer, error) {
		a.Logger.Bodyf("Executing %s %s", filepath.Base(a.Command), strings.Join(a.Arguments, " "))
		if err := a.Executor.Execute(effect.Execution{
			Command: a.Command,
			Args:    a.Arguments,
			Dir:     a.ApplicationPath,
			Stdout:  a.Logger.InfoWriter(),
			Stderr:  a.Logger.InfoWriter(),
		}); err != nil {
			return libcnb.Layer{}, fmt.Errorf("error running build\n%w", err)
		}

		artifact, err := a.ArtifactResolver.Resolve(a.ApplicationPath)
		if err != nil {
			return libcnb.Layer{}, fmt.Errorf("unable to resolve artifact\n%w", err)
		}

		in, err := os.Open(artifact)
		if err != nil {
			return libcnb.Layer{}, fmt.Errorf("unable to open %s\n%w", artifact, err)
		}
		defer in.Close()

		file := filepath.Join(layer.Path, "application.zip")
		if err := sherpa.CopyFile(in, file); err != nil {
			return libcnb.Layer{}, fmt.Errorf("unable to copy %s to %s\n%w", artifact, file, err)
		}

		return layer, nil
	}, libpak.CacheLayer)
	if err != nil {
		return libcnb.Layer{}, fmt.Errorf("unable to contribute application layer\n%w", err)
	}

	entry, err := a.Cache.AsBuildpackPlanEntry()
	if err != nil {
		return libcnb.Layer{}, fmt.Errorf("unable to generate build dependencies\n%w", err)
	}
	entry.Metadata["layer"] = a.Cache.Name()
	a.Plan.Entries = append(a.Plan.Entries, entry)

	a.Logger.Header("Removing source code")
	cs, err := ioutil.ReadDir(a.ApplicationPath)
	if err != nil {
		return libcnb.Layer{}, fmt.Errorf("unable to list children of %s\n%w", a.ApplicationPath, err)
	}
	for _, c := range cs {
		file := filepath.Join(a.ApplicationPath, c.Name())
		if err := os.RemoveAll(file); err != nil {
			return libcnb.Layer{}, fmt.Errorf("unable to remove %s\n%w", file, err)
		}
	}

	file := filepath.Join(layer.Path, "application.zip")
	in, err := os.Open(file)
	if err != nil {
		return libcnb.Layer{}, fmt.Errorf("unable to open %s\n%w", file, err)
	}
	defer in.Close()

	if err := crush.ExtractZip(in, a.ApplicationPath, 0); err != nil {
		return libcnb.Layer{}, fmt.Errorf("unable to extract %s\n%w", file, err)
	}

	return layer, nil
}

func (Application) Name() string {
	return "application"
}
