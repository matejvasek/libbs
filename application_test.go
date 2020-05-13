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

package libbs_test

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildpacks/libcnb"
	. "github.com/onsi/gomega"
	"github.com/paketo-buildpacks/libbs"
	"github.com/paketo-buildpacks/libjvm"
	"github.com/paketo-buildpacks/libpak"
	"github.com/paketo-buildpacks/libpak/bard"
	"github.com/paketo-buildpacks/libpak/effect"
	"github.com/paketo-buildpacks/libpak/effect/mocks"
	"github.com/paketo-buildpacks/libpak/sherpa"
	"github.com/sclevine/spec"
	"github.com/stretchr/testify/mock"
)

func testApplication(t *testing.T, context spec.G, it spec.S) {
	var (
		Expect = NewWithT(t).Expect

		cache       libbs.Cache
		ctx         libcnb.BuildContext
		application libbs.Application
		executor    *mocks.Executor
		plan        *libcnb.BuildpackPlan
	)

	it.Before(func() {
		var err error

		ctx.Application.Path, err = ioutil.TempDir("", "application-application")
		Expect(err).NotTo(HaveOccurred())

		ctx.Layers.Path, err = ioutil.TempDir("", "application-layers")
		Expect(err).NotTo(HaveOccurred())

		cache.Path, err = ioutil.TempDir("", "application-cache")
		Expect(err).NotTo(HaveOccurred())

		plan = &libcnb.BuildpackPlan{}

		artifactResolver := libbs.ArtifactResolver{
			ConfigurationResolver: libpak.ConfigurationResolver{
				Configurations: []libpak.BuildpackConfiguration{{Default: "*"}},
			},
		}

		executor = &mocks.Executor{}

		application = libbs.Application{
			ApplicationPath:  ctx.Application.Path,
			Arguments:        []string{"test-argument"},
			ArtifactResolver: artifactResolver,
			Cache:            cache,
			Command:          "test-command",
			Executor:         executor,
			LayerContributor: libpak.NewLayerContributor("test", map[string]interface{}{}),
			Logger:           bard.Logger{},
			Plan:             plan,
		}
	})

	it.After(func() {
		Expect(os.RemoveAll(ctx.Application.Path)).To(Succeed())
		Expect(os.RemoveAll(ctx.Layers.Path)).To(Succeed())
		Expect(os.RemoveAll(cache.Path)).To(Succeed())
	})

	context("ExpectedMetadata", func() {

		it.Before(func() {
			executor.On("Execute", mock.Anything).Run(func(args mock.Arguments) {
				execution := args.Get(0).(effect.Execution)
				_, err := execution.Stdout.Write([]byte("javac some-version"))
				Expect(err).NotTo(HaveOccurred())
			}).Return(nil)
		})

		it("adds file list", func() {
			file := filepath.Join(ctx.Application.Path, "some-file")
			Expect(ioutil.WriteFile(file, []byte{}, 0644)).To(Succeed())

			file, err := filepath.EvalSymlinks(file)
			Expect(err).NotTo(HaveOccurred())

			metadata, err := application.ExpectedMetadata(map[string]interface{}{})
			Expect(err).NotTo(HaveOccurred())

			fileEntries, ok := metadata["files"].([]sherpa.FileEntry)
			Expect(ok).To(BeTrue())
			Expect(fileEntries[0].Path).To(Equal(file))
		})

		it("adds args", func() {
			metadata, err := application.ExpectedMetadata(map[string]interface{}{})
			Expect(err).NotTo(HaveOccurred())
			Expect(metadata["arguments"]).To(Equal([]string{"test-argument"}))
		})

		it("adds artifact pattern", func() {
			metadata, err := application.ExpectedMetadata(map[string]interface{}{})
			Expect(err).NotTo(HaveOccurred())
			Expect(metadata["artifact-pattern"]).To(Equal("*"))
		})

		it("adds java version", func() {
			metadata, err := application.ExpectedMetadata(map[string]interface{}{})
			Expect(err).NotTo(HaveOccurred())
			Expect(metadata["java-version"]).To(Equal("some-version"))
		})

		it("accepts arbitrary metadata", func() {
			metadata, err := application.ExpectedMetadata(map[string]interface{}{"test-key": "test-value"})
			Expect(err).NotTo(HaveOccurred())
			Expect(metadata["test-key"]).To(Equal("test-value"))
		})

	})

	it("contributes layer", func() {
		in, err := os.Open(filepath.Join("testdata", "stub-application.jar"))
		Expect(err).NotTo(HaveOccurred())
		out, err := os.OpenFile(filepath.Join(ctx.Application.Path, "stub-application.jar"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		Expect(err).NotTo(HaveOccurred())
		_, err = io.Copy(out, in)
		Expect(err).NotTo(HaveOccurred())
		Expect(in.Close()).To(Succeed())
		Expect(out.Close()).To(Succeed())
		Expect(ioutil.WriteFile(filepath.Join(cache.Path, "test-file-1.1.1.jar"), []byte{}, 0644)).To(Succeed())

		application.Logger = bard.NewLogger(ioutil.Discard)
		executor.On("Execute", mock.Anything).Return(nil)

		layer, err := ctx.Layers.Layer("test-layer")
		Expect(err).NotTo(HaveOccurred())

		layer, err = application.Contribute(layer)
		Expect(err).NotTo(HaveOccurred())

		Expect(layer.Cache).To(BeTrue())

		e := executor.Calls[0].Arguments[0].(effect.Execution)
		Expect(e.Command).To(Equal("test-command"))
		Expect(e.Args).To(Equal([]string{"test-argument"}))
		Expect(e.Dir).To(Equal(ctx.Application.Path))
		Expect(e.Stdout).NotTo(BeNil())
		Expect(e.Stderr).NotTo(BeNil())

		Expect(filepath.Join(layer.Path, "application.zip")).To(BeARegularFile())
		Expect(filepath.Join(ctx.Application.Path, "stub-application.jar")).NotTo(BeAnExistingFile())
		Expect(filepath.Join(ctx.Application.Path, "fixture-marker")).To(BeARegularFile())

		Expect(plan).To(Equal(&libcnb.BuildpackPlan{
			Entries: []libcnb.BuildpackPlanEntry{
				{
					Name: "build-dependencies",
					Metadata: map[string]interface{}{
						"dependencies": []libjvm.MavenJAR{
							{
								Name:    "test-file",
								Version: "1.1.1",
								SHA256:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
							},
						},
					},
				},
			},
		}))
	})

}
