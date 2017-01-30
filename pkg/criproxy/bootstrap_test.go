/*
Copyright 2016 Mirantis

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

package criproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	dockerstdcopy "github.com/docker/docker/pkg/stdcopy"
	dockerclient "github.com/docker/engine-api/client"
	dockertypes "github.com/docker/engine-api/types"
	dockercontainer "github.com/docker/engine-api/types/container"

	// TODO: use client-go
	"k8s.io/kubernetes/pkg/api"
	_ "k8s.io/kubernetes/pkg/api/testapi"
	cfg "k8s.io/kubernetes/pkg/apis/componentconfig/v1alpha1"
	clientsetfake "k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset/fake"
	testingcore "k8s.io/kubernetes/pkg/client/testing/core"
	"k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/stats"
)

func getStruct(s interface{}) reflect.Value {
	v := reflect.ValueOf(s)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		log.Panicf("struct or pointer to struct expected, but got %#v", s)
	}
	return v
}

func diffStructs(old, new interface{}) map[string]interface{} {
	vOld := getStruct(old)
	vNew := getStruct(new)
	if vOld.Type() != vNew.Type() {
		log.Panicf("got different struct types")
	}
	r := make(map[string]interface{})
	for i, n := 0, vOld.NumField(); i < n; i++ {
		if !reflect.DeepEqual(vOld.Field(i).Interface(), vNew.Field(i).Interface()) {
			r[vOld.Type().Field(i).Name] = vNew.Field(i).Interface()
		}
	}
	return r
}

func mustMarshalJson(data interface{}) []byte {
	text, err := json.Marshal(data)
	if err != nil {
		panic("failed to marshal json")
	}
	return text
}

func TestPatchKubeletConfig(t *testing.T) {
	var kubeCfg cfg.KubeletConfiguration
	api.Scheme.Default(&kubeCfg)
	kubeCfg.CNIConfDir = "/etc/kubernetes/cni/net.d"
	kubeCfg.CNIBinDir = "/usr/lib/kubernetes/cni/bin"

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v interface{}
		switch r.URL.Path {
		case "/configz":
			v = map[string]interface{}{"componentconfig": &kubeCfg}
		case "/stats/summary":
			v = &stats.Summary{Node: stats.NodeStats{NodeName: "samplenode"}}
		default:
			http.NotFound(w, r)
			return
		}
		w.Write(mustMarshalJson(v))
	}))

	tc := clientsetfake.NewSimpleClientset()

	tmpDir, err := ioutil.TempDir("", "criproxy-test")
	if err != nil {
		t.Fatalf("TempDir(): %v", err)
	}
	defer os.RemoveAll(tmpDir)
	confFileName := path.Join(tmpDir, "kubelet.conf")

	b := NewBootstrap(&BootstrapConfig{
		ConfigzBaseUrl:  s.URL,
		StatsBaseUrl:    s.URL,
		SavedConfigPath: confFileName,
	}, tc)

	if needToPatch, err := b.needToPatch(); err != nil {
		t.Errorf("needToPatch(): %v", err)
	} else if !needToPatch {
		t.Errorf("needToPatch() reports no need to patch for unpatched config")
	}

	if err := b.obtainKubeletConfig(); err != nil {
		t.Fatalf("obtainKubeletConfig(): %v", err)
	}

	if err := b.saveKubeletConfig(); err != nil {
		t.Fatalf("saveKubeletConfig(): %v", err)
	}

	savedCfg, err := LoadKubeletConfig(confFileName)
	if err != nil {
		t.Fatalf("loadKubeletConfig: %v", err)
	}

	if err := b.patchKubeletConfig(); err != nil {
		t.Fatalf("patchKubeletConfig(): %v", err)
	}

	if string(mustMarshalJson(savedCfg)) != string(mustMarshalJson(kubeCfg)) {
		t.Fatalf("bad saved kubelet config: %s", spew.Sdump(savedCfg))
	}

	if dockerEndpoint, err := b.dockerEndpoint(); err != nil {
		t.Errorf("dockerEndpoint(): %v", err)
	} else if dockerEndpoint != "unix:///var/run/docker.sock" {
		t.Errorf("unexpected dockerEndpoint: %q", dockerEndpoint)
	}

	actions := tc.Fake.Actions()
	if len(actions) != 1 || actions[0].GetNamespace() != "kube-system" || actions[0].GetVerb() != "create" {
		t.Fatalf("invalid clientset actions: %s", spew.Sdump(actions))
	}
	o := actions[0].(testingcore.CreateAction).GetObject()
	cfgMap, ok := o.(*api.ConfigMap)
	if !ok || cfgMap.Name != "kubelet-samplenode" || cfgMap.Namespace != "kube-system" || cfgMap.Data["kubelet.config"] == "" {
		t.Fatalf("invalid object created: %s", spew.Sdump(o))
	}

	var newKubeCfg cfg.KubeletConfiguration
	if err := json.Unmarshal([]byte(cfgMap.Data["kubelet.config"]), &newKubeCfg); err != nil {
		t.Fatalf("Failed to unmarshal kubelet config: %v", err)
	}

	expectedDiff := map[string]interface{}{
		"EnableCRI":             true,
		"ContainerRuntime":      "remote",
		"RemoteRuntimeEndpoint": "/run/criproxy.sock",
		"RemoteImageEndpoint":   "/run/criproxy.sock",
	}
	diff := diffStructs(&kubeCfg, &newKubeCfg)
	if !reflect.DeepEqual(diff, expectedDiff) {
		m, err := json.MarshalIndent(diff, "", "  ")
		if err != nil {
			t.Fatalf("can't marshal struct diff: %v", err)
		}
		t.Errorf("bad kubelet config diff:\n%s", m)
	}

	if needToPatch, err := b.needToPatch(); err != nil {
		t.Errorf("needToPatch(): %v", err)
	} else if needToPatch {
		t.Errorf("needToPatch() reports the need to patch for the patched config")
	}
}

// This is a simple way to do it (untested), but it requires docker client to be present in build container:
//
// func runCommandViaDocker(t *testing.T, shellCommand, stdin string) string {
// 	cmd := exec.Command("docker", "exec", "-i", "-v", "/tmp:/tmp", "--rm", busyboxImageName, "/bin/sh", "-c", shellCommand)
// 	if stdin != "" {
// 		cmd.Stdin = strings.NewReader(stdin)
// 	}
// 	out, err := cmd.Output()
// 	if err != nil {
// 		t.Fatalf("failed to run %q via docker: %v", shellCommand, err)
// 	}
// 	return strings.TrimSpace(string(out))
// }

func removeContainer(t *testing.T, containerId string) {
	ctx := context.Background()
	client, err := dockerclient.NewEnvClient()
	if err != nil {
		t.Fatalf("Can't create Docker client: %v", err)
	}
	if err := client.ContainerRemove(ctx, containerId, dockertypes.ContainerRemoveOptions{
		Force: true,
	}); err != nil {
		t.Fatalf("ContainerRemove(): %v")
	}
}

func runCommandViaDocker(t *testing.T, shellCommand, stdin string) string {
	ctx := context.Background()
	client, err := dockerclient.NewEnvClient()
	if err != nil {
		t.Fatalf("Can't create Docker client: %v", err)
	}

	if err := pullImage(ctx, client, BusyboxImageName, false); err != nil {
		t.Fatalf("Failed to pull busybox image: %v", err)
	}

	haveStdin := stdin != ""
	containerName := fmt.Sprintf("criproxy-test-%d", time.Now().UnixNano())
	resp, err := client.ContainerCreate(ctx, &dockercontainer.Config{
		Image:       BusyboxImageName,
		Cmd:         []string{"/bin/sh", "-c", shellCommand},
		AttachStdin: haveStdin,
		OpenStdin:   haveStdin,
		StdinOnce:   haveStdin,
	}, &dockercontainer.HostConfig{
		Binds: []string{"/tmp:/tmp", "/run:/run"},
	}, nil, containerName)
	containerId := resp.ID

	defer removeContainer(t, containerId)

	var hijacked dockertypes.HijackedResponse
	if haveStdin {
		hijacked, err = client.ContainerAttach(ctx, containerId, dockertypes.ContainerAttachOptions{
			Stdin:  true,
			Stream: true,
		})
		if err != nil {
			t.Fatalf("ContainerAttach(): %v", err)
		}
	}

	if err := client.ContainerStart(ctx, containerId); err != nil {
		if hijacked.Conn != nil {
			hijacked.Conn.Close()
		}
		t.Fatalf("failed to start CRI proxy container: %v", err)
	}

	if haveStdin {
		if _, err := hijacked.Conn.Write([]byte(stdin)); err != nil {
			hijacked.Conn.Close()
			t.Fatalf("Failed to write to hijacked connection: %v", err)
		}

		if err := hijacked.Conn.Close(); err != nil {
			t.Fatalf("Failed to close hijacked connection: %v", err)
		}
	}

	status, err := client.ContainerWait(ctx, containerId)
	if err != nil {
		t.Fatalf("ContainerWait(): %v", err)
	}

	// We prefer to grabs logs there instead of attaching to container's stdout/stderr
	// because of this issue: https://github.com/docker/docker/issues/29285
	logs, err := client.ContainerLogs(ctx, containerId, dockertypes.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		t.Fatalf("ContainerLogs(): %v", err)
	}
	defer logs.Close()

	var outBuf, errBuf bytes.Buffer
	_, err = dockerstdcopy.StdCopy(&outBuf, &errBuf, logs)
	if err != nil {
		t.Fatalf("stdcopy: %v", err)
	}

	if status != 0 {
		t.Fatalf("%q: container exited with non-zero code: %d. Stderr:\n%s", shellCommand, status, errBuf.String())
	}

	return strings.TrimSpace(outBuf.String())
}

func writeScript(t *testing.T, text string) string {
	return runCommandViaDocker(t, "name=`mktemp`;cat >$name;chmod +x $name;echo $name", text)
}

const (
	fakeProxyScriptText = `#!/bin/sh
if [ ! -f @@ ]; then
  # the checker will have to wait
  echo >&2 "fail: host /run not accessible"
elif [ "$1" != a -o "$2" != b ]; then
  echo "fail: args not passed" >@@
elif [ ! -S /var/run/docker.sock ]; then
  echo "fail: can't locate /var/run/docker.sock" >@@
else
  echo ok > @@
fi
sleep 10000
`
	checkerScriptText = `#!/bin/sh
for i in $(seq 1 60); do
  if grep fail @@; then
    cat >&2 @@
    exit 1
  elif grep ok @@; then
    exit 0
  fi
  sleep 1
done
echo >&2 "Timed out wating for test results"
exit 1
`
)

func TestInstallCriProxyContainer(t *testing.T) {
	endpointToPass := os.Getenv("CRIPROXY_TEST_REMOTE_DOCKER_ENDPOINT")
	if endpointToPass == "" {
		t.Skip("You must set CRIPROXY_TEST_REMOTE_DOCKER_ENDPOINT to run this test")
	}
	dockerEndpoint := os.Getenv("DOCKER_HOST")
	if dockerEndpoint == "" {
		dockerEndpoint = dockerclient.DefaultDockerHost
	}

	var rm []string
	checkFile := runCommandViaDocker(t, "mktemp /run/criproxy-check-XXXXXX", "")
	rm = append(rm, checkFile)
	defer runCommandViaDocker(t, "rm -f "+strings.Join(rm, " "), "")

	fakeProxyScript := writeScript(t, strings.Replace(fakeProxyScriptText, "@@", checkFile, -1))
	rm = append(rm, fakeProxyScript)

	checkerScript := writeScript(t, strings.Replace(checkerScriptText, "@@", checkFile, -1))
	rm = append(rm, checkerScript)

	b := NewBootstrap(&BootstrapConfig{
		ProxyPath: fakeProxyScript,
		ProxyArgs: []string{"a", "b"},
	}, nil)
	containerId, err := b.installCriProxyContainer(dockerEndpoint, endpointToPass)
	if err != nil {
		t.Fatalf("Failed to install CRI proxy container: %v", err)
	}
	defer removeContainer(t, containerId)

	runCommandViaDocker(t, checkerScript, "")
}

// TODO: concerning getting node name et al:
// https://github.com/kubernetes/kubernetes/blob/v1.5.2/test/e2e_node/util.go#L48
// That's 'readOnlyPort' -- may use this to get node name,
// then access configz via the proxy. (/stats/exec is also available via configz)
//
// "k8s.io/kubernetes/pkg/api" (perhaps try using client-go instead?)
// api.Scheme.Default
// TODO: test 'don't patch config again'
// TODO: patching kubelet config from existing configmap
// (see setKubeletConfiguration in k8s' test/e2e_node/util.go)
