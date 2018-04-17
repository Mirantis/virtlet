package version

import (
	"testing"

	"github.com/Mirantis/virtlet/tests/gm"
)

func withNodeName(name string, v Info) Info {
	v.NodeName = name
	return v
}

var (
	sampleVersionInfo = Info{
		Major:        "1",
		Minor:        "0",
		GitVersion:   "v1.0.0-6+318faff6ad0609-dirty",
		GitCommit:    "318faff6ad060954387c1ff594bcbb4bb128577a",
		GitTreeState: "dirty",
		BuildDate:    "2018-04-16T21:02:05Z",
		GoVersion:    "go1.8.3",
		Compiler:     "gc",
		Platform:     "darwin/amd64",
		ImageTag:     "ivan4th_version",
	}
	linuxVersionInfo = Info{
		NodeName:     "kube-node-1",
		Major:        "1",
		Minor:        "0",
		GitVersion:   "v1.0.0-6+318faff6ad0609",
		GitCommit:    "318faff6ad060954387c1ff594bcbb4bb128577a",
		GitTreeState: "clean",
		BuildDate:    "2018-04-16T21:02:05Z",
		GoVersion:    "go1.8.3",
		Compiler:     "gc",
		Platform:     "linux/amd64",
	}
	sampleClusterVersionInfo = ClusterVersionInfo{
		ClientVersion: sampleVersionInfo,
		NodeVersions: []Info{
			withNodeName("kube-node-1", linuxVersionInfo),
			withNodeName("kube-node-2", linuxVersionInfo),
		},
	}
	emptyClusterVersionInfo = ClusterVersionInfo{
		ClientVersion: sampleVersionInfo,
	}
)

func TestVersion(t *testing.T) {
	for _, tc := range []struct {
		format string
		error  bool
		wrap   func([]byte) gm.Verifier
	}{
		{
			format: "text",
			error:  false,
		},
		{
			format: "short",
			error:  false,
		},
		{
			format: "json",
			error:  false,
			wrap:   func(bs []byte) gm.Verifier { return gm.NewJSONVerifier(bs) },
		},
		{
			format: "yaml",
			error:  false,
			wrap:   func(bs []byte) gm.Verifier { return gm.NewYamlVerifier(bs) },
		},
		{
			format: "foobar",
			error:  true,
		},
	} {
		t.Run(tc.format, func(t *testing.T) {
			out, err := sampleVersionInfo.ToBytes(tc.format)
			switch {
			case err != nil && tc.error:
				// ok
			case err != nil:
				t.Errorf("ToBytes(): unexpected error: %v", err)
			case tc.wrap != nil:
				gm.Verify(t, tc.wrap(out))
			default:
				gm.Verify(t, out)
			}
		})
	}
}

func TestClusterVersion(t *testing.T) {
	for _, tc := range []struct {
		format string
		error  bool
		wrap   func([]byte) gm.Verifier
	}{
		{
			format: "text",
			error:  false,
		},
		{
			format: "short",
			error:  false,
		},
		{
			format: "json",
			error:  false,
			wrap:   func(bs []byte) gm.Verifier { return gm.NewJSONVerifier(bs) },
		},
		{
			format: "yaml",
			error:  false,
			wrap:   func(bs []byte) gm.Verifier { return gm.NewYamlVerifier(bs) },
		},
		{
			format: "foobar",
			error:  true,
		},
	} {
		for _, sub := range []struct {
			suffix string
			v      ClusterVersionInfo
		}{{"", sampleClusterVersionInfo}, {"/empty", emptyClusterVersionInfo}} {
			t.Run(tc.format+sub.suffix, func(t *testing.T) {
				out, err := sub.v.ToBytes(tc.format)
				switch {
				case err != nil && tc.error:
					// ok
				case err != nil:
					t.Errorf("ToBytes(): unexpected error: %v", err)
				case tc.wrap != nil:
					gm.Verify(t, tc.wrap(out))
				default:
					gm.Verify(t, out)
				}
			})
		}
	}
}

func TestNodeConsistency(t *testing.T) {
	if !emptyClusterVersionInfo.AreNodesConsistent() {
		t.Errorf("unexpected node inconsistency in %#v (no nodes)", emptyClusterVersionInfo)
	}
	if !sampleClusterVersionInfo.AreNodesConsistent() {
		t.Errorf("unexpected node inconsistency in %#v", sampleClusterVersionInfo)
	}
	v := ClusterVersionInfo{
		ClientVersion: sampleVersionInfo,
		NodeVersions: []Info{
			withNodeName("kube-node-1", linuxVersionInfo),
			withNodeName("kube-node-2", linuxVersionInfo),
		},
	}
	v.NodeVersions[1].BuildDate = "2018-04-17T07:16:01Z"
	if v.AreNodesConsistent() {
		t.Errorf("nodes aren't expected to be consistent in %#v", v)
	}
}
