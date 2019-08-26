/*
Copyright 2019 The Skaffold Authors

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

package integration

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/GoogleContainerTools/skaffold/integration/skaffold"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/config"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/kubectl"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/runner/runcontext"

	kubectx "github.com/GoogleContainerTools/skaffold/pkg/skaffold/kubernetes/context"
)

func TestGeneratePipelineOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	if ShouldRunGCPOnlyTests() {
		t.Skip("skipping test that is not gcp only")
	}

	tests := []struct {
		description string
		dir         string
		responses   []byte
	}{
		{
			description: "no profiles",
			dir:         "testdata/generate_pipeline/no_profiles",
			responses:   []byte("y"),
		},
		{
			description: "existing oncluster profile",
			dir:         "testdata/generate_pipeline/existing_oncluster",
			responses:   []byte(""),
		},
		{
			description: "existing other profile",
			dir:         "testdata/generate_pipeline/existing_other",
			responses:   []byte("y"),
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			originalConfig, err := ioutil.ReadFile(test.dir + "/skaffold.yaml")
			if err != nil {
				t.Error("error reading skaffold yaml")
			}
			defer ioutil.WriteFile(test.dir+"/skaffold.yaml", originalConfig, 0755)

			skaffoldEnv := []string{
				"PIPELINE_GIT_URL=this-is-a-test",
				"PIPELINE_SKAFFOLD_VERSION=test-version",
			}
			skaffold.GeneratePipeline().WithStdin([]byte("y\n")).WithEnv(skaffoldEnv).InDir(test.dir).RunOrFail(t)

			checkFileContents(t, test.dir+"/expectedSkaffold.yaml", test.dir+"/skaffold.yaml")
			checkFileContents(t, test.dir+"/expectedPipeline.yaml", test.dir+"/pipeline.yaml")
		})
	}
}

func TestGeneratePipelineE2E(t *testing.T) {
	tests := []struct {
		description string
		dir         string
		responses   []byte
		pods        []string
	}{
		{
			description: "getting-started",
			dir:         "testdata/generate_pipeline/getting_started",
			responses:   []byte("y"),
			pods:        []string{"getting-started"},
		},
	}
	for _, test := range tests {

		if testing.Short() {
			t.Skip("skipping integration test")
		}
		if !ShouldRunGCPOnlyTests() {
			t.Skip("skipping gcp only test")
		}

		t.Run(test.description, func(t *testing.T) {
			skaffoldEnv := []string{
				"PIPELINE_GIT_URL=https://github.com/marlon-gamez/getting-started.git",
				"PIPELINE_SKAFFOLD_VERSION=v0.35.0",
			}
			skaffold.GeneratePipeline().WithStdin([]byte("y\n")).WithEnv(skaffoldEnv).InDir(test.dir).RunOrFail(t)

			// run pipeline on cluster
			ns, client, _ := SetupNamespace(t)
			//defer deleteNs()

			cfg, err := kubectx.CurrentConfig()
			if err != nil {
				t.Fatal(err)
			}

			cli := kubectl.NewFromRunContext(&runcontext.RunContext{
				KubeContext: cfg.CurrentContext,
				Opts: config.SkaffoldOptions{
					Namespace: ns.Name,
				},
			})

			copySecretToNamespace(t, cli, "default", "kaniko-secret")

			// kubectl apply -f pipeline.yaml
			out, err := cli.RunOut(context.Background(), "apply", "-f", test.dir+"/pipeline.yaml")
			if err != nil {
				t.Fatal(err)
			}
			fmt.Println(string(out))

			// kubectl apply -f pipelinerun.yaml
			out, err = cli.RunOut(context.Background(), "apply", "-f", test.dir+"/pipelinerun.yaml")
			if err != nil {
				t.Fatal(err)
			}
			fmt.Println(string(out))

			// wait for pod that comes from deployment
			client.WaitForPodsReady(test.pods...)

		})
	}
}

func checkFileContents(t *testing.T, wantFile, gotFile string) {
	wantContents, err := ioutil.ReadFile(wantFile)
	if err != nil {
		t.Errorf("Error while reading contents of file %s", wantFile)
	}
	gotContents, err := ioutil.ReadFile(gotFile)
	if err != nil {
		t.Errorf("Error while reading contents of file %s", gotFile)
	}

	if !bytes.Equal(wantContents, gotContents) {
		t.Errorf("Contents of %s did not match those of %s\ngot:%s\nwant:%s", gotFile, wantFile, string(gotContents), string(wantContents))
	}
}

func copySecretToNamespace(t *testing.T, cli *kubectl.CLI, from, secretName string) {
	pipeOut := bytes.NewBuffer([]byte{})
	pipeIn := bytes.NewBuffer([]byte{})

	getSecret := cli.CommandWithNamespaceArg(context.Background(), "get", from, "secret", secretName, "-o", "yaml")
	applySecret := cli.Command(context.Background(), "apply", "-f", "-")

	getSecret.Stdout = pipeOut
	applySecret.Stdin = pipeIn

	t.Log("Running kubectl get secret")
	if err := getSecret.Run(); err != nil {
		t.Fatal(err)
	}

	pipeIn.WriteString(strings.Replace(string(pipeOut.Bytes()), "namespace: default", fmt.Sprintf("namespace: %s", cli.Namespace), 1))

	t.Log("Running kubectl apply")
	if err := applySecret.Run(); err != nil {
		t.Fatal(err)
	}
}
