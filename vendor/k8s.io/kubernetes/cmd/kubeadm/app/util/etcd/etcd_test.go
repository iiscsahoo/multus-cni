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

package etcd

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	kubeadmapi "k8s.io/kubernetes/cmd/kubeadm/app/apis/kubeadm"
	"k8s.io/kubernetes/cmd/kubeadm/app/constants"
	testutil "k8s.io/kubernetes/cmd/kubeadm/test"
)

const (
	secureEtcdPod = `# generated by kubeadm v1.10.0
apiVersion: v1
kind: Pod
metadata:
  annotations:
    scheduler.alpha.kubernetes.io/critical-pod: ""
  creationTimestamp: null
  labels:
    component: etcd
    tier: control-plane
  name: etcd
  namespace: kube-system
spec:
  containers:
  - command:
    - etcd
    - --advertise-client-urls=https://127.0.0.1:2379
    - --data-dir=/var/lib/etcd
    - --peer-key-file=/etc/kubernetes/pki/etcd/peer.key
    - --peer-trusted-ca-file=/etc/kubernetes/pki/etcd/ca.crt
    - --listen-client-urls=https://127.0.0.1:2379
    - --peer-client-cert-auth=true
    - --cert-file=/etc/kubernetes/pki/etcd/server.crt
    - --key-file=/etc/kubernetes/pki/etcd/server.key
    - --trusted-ca-file=/etc/kubernetes/pki/etcd/ca.crt
    - --peer-cert-file=/etc/kubernetes/pki/etcd/peer.crt
    - --client-cert-auth=true
    image: k8s.gcr.io/etcd-amd64:3.1.12
    livenessProbe:
      exec:
        command:
        - /bin/sh
        - -ec
        - ETCDCTL_API=3 etcdctl --endpoints=127.0.0.1:2379 --cacert=/etc/kubernetes/pki/etcd/ca.crt
          --cert=/etc/kubernetes/pki/etcd/healthcheck-client.crt --key=/etc/kubernetes/pki/etcd/healthcheck-client.key
          get foo
      failureThreshold: 8
      initialDelaySeconds: 15
      timeoutSeconds: 15
    name: etcd
    resources: {}
    volumeMounts:
    - mountPath: /var/lib/etcd
      name: etcd-data
    - mountPath: /etc/kubernetes/pki/etcd
      name: etcd-certs
  hostNetwork: true
  volumes:
  - hostPath:
      path: /var/lib/etcd
      type: DirectoryOrCreate
    name: etcd-data
  - hostPath:
      path: /etc/kubernetes/pki/etcd
      type: DirectoryOrCreate
    name: etcd-certs
status: {}
`
	secureExposedEtcdPod = `
apiVersion: v1
kind: Pod
metadata:
  annotations:
    scheduler.alpha.kubernetes.io/critical-pod: ""
  creationTimestamp: null
  labels:
    component: etcd
    tier: control-plane
  name: etcd
  namespace: kube-system
spec:
  containers:
  - command:
    - etcd
    - --advertise-client-urls=https://10.0.5.5:2379
    - --data-dir=/var/lib/etcd
    - --peer-key-file=/etc/kubernetes/pki/etcd/peer.key
    - --peer-trusted-ca-file=/etc/kubernetes/pki/etcd/ca.crt
    - --listen-client-urls=https://[::0:0]:2379
    - --peer-client-cert-auth=true
    - --cert-file=/etc/kubernetes/pki/etcd/server.crt
    - --key-file=/etc/kubernetes/pki/etcd/server.key
    - --trusted-ca-file=/etc/kubernetes/pki/etcd/ca.crt
    - --peer-cert-file=/etc/kubernetes/pki/etcd/peer.crt
    - --client-cert-auth=true
    image: k8s.gcr.io/etcd-amd64:3.1.12
    livenessProbe:
      exec:
        command:
        - /bin/sh
        - -ec
        - ETCDCTL_API=3 etcdctl --endpoints=https://[::1]:2379 --cacert=/etc/kubernetes/pki/etcd/ca.crt
          --cert=/etc/kubernetes/pki/etcd/healthcheck-client.crt --key=/etc/kubernetes/pki/etcd/healthcheck-client.key
          get foo
      failureThreshold: 8
      initialDelaySeconds: 15
      timeoutSeconds: 15
    name: etcd
    resources: {}
    volumeMounts:
    - mountPath: /var/lib/etcd
      name: etcd-data
    - mountPath: /etc/kubernetes/pki/etcd
      name: etcd-certs
  hostNetwork: true
  volumes:
  - hostPath:
      path: /var/lib/etcd
      type: DirectoryOrCreate
    name: etcd-data
  - hostPath:
      path: /etc/kubernetes/pki/etcd
      type: DirectoryOrCreate
    name: etcd-certs
status: {}
`
	insecureEtcdPod = `# generated by kubeadm v1.9.6
apiVersion: v1
kind: Pod
metadata:
  annotations:
    scheduler.alpha.kubernetes.io/critical-pod: ""
  creationTimestamp: null
  labels:
    component: etcd
    tier: control-plane
  name: etcd
  namespace: kube-system
spec:
  containers:
  - command:
    - etcd
    - --listen-client-urls=http://127.0.0.1:2379
    - --advertise-client-urls=http://127.0.0.1:2379
    - --data-dir=/var/lib/etcd
    image: gcr.io/google_containers/etcd-amd64:3.1.11
    livenessProbe:
      failureThreshold: 8
      httpGet:
        host: 127.0.0.1
        path: /health
        port: 2379
        scheme: HTTP
      initialDelaySeconds: 15
      timeoutSeconds: 15
    name: etcd
    resources: {}
    volumeMounts:
    - mountPath: /var/lib/etcd
      name: etcd
  hostNetwork: true
  volumes:
  - hostPath:
      path: /var/lib/etcd
      type: DirectoryOrCreate
    name: etcd
status: {}
`
	invalidPod = `---{ broken yaml @@@`
)

func TestPodManifestHasTLS(t *testing.T) {
	tests := []struct {
		description   string
		podYaml       string
		hasTLS        bool
		expectErr     bool
		writeManifest bool
	}{
		{
			description:   "secure etcd returns true",
			podYaml:       secureEtcdPod,
			hasTLS:        true,
			writeManifest: true,
			expectErr:     false,
		},
		{
			description:   "secure exposed etcd returns true",
			podYaml:       secureExposedEtcdPod,
			hasTLS:        true,
			writeManifest: true,
			expectErr:     false,
		},
		{
			description:   "insecure etcd returns false",
			podYaml:       insecureEtcdPod,
			hasTLS:        false,
			writeManifest: true,
			expectErr:     false,
		},
		{
			description:   "invalid pod fails to unmarshal",
			podYaml:       invalidPod,
			hasTLS:        false,
			writeManifest: true,
			expectErr:     true,
		},
		{
			description:   "non-existent file returns error",
			podYaml:       ``,
			hasTLS:        false,
			writeManifest: false,
			expectErr:     true,
		},
	}

	for _, rt := range tests {
		tmpdir := testutil.SetupTempDir(t)
		defer os.RemoveAll(tmpdir)

		manifestPath := filepath.Join(tmpdir, "etcd.yaml")
		if rt.writeManifest {
			err := ioutil.WriteFile(manifestPath, []byte(rt.podYaml), 0644)
			if err != nil {
				t.Fatalf("Failed to write pod manifest\n%s\n\tfatal error: %v", rt.description, err)
			}
		}

		hasTLS, actualErr := PodManifestsHaveTLS(tmpdir)
		if (actualErr != nil) != rt.expectErr {
			t.Errorf(
				"PodManifestHasTLS failed\n%s\n\texpected error: %t\n\tgot: %t\n\tactual error: %v",
				rt.description,
				rt.expectErr,
				(actualErr != nil),
				actualErr,
			)
		}

		if hasTLS != rt.hasTLS {
			t.Errorf("PodManifestHasTLS failed\n%s\n\texpected hasTLS: %t\n\tgot: %t", rt.description, rt.hasTLS, hasTLS)
		}
	}
}

func TestCheckConfigurationIsHA(t *testing.T) {
	var tests = []struct {
		name     string
		cfg      *kubeadmapi.Etcd
		expected bool
	}{
		{
			name: "HA etcd",
			cfg: &kubeadmapi.Etcd{
				External: &kubeadmapi.ExternalEtcd{
					Endpoints: []string{"10.100.0.1:2379", "10.100.0.2:2379", "10.100.0.3:2379"},
				},
			},
			expected: true,
		},
		{
			name: "single External etcd",
			cfg: &kubeadmapi.Etcd{
				External: &kubeadmapi.ExternalEtcd{
					Endpoints: []string{"10.100.0.1:2379"},
				},
			},
			expected: false,
		},
		{
			name: "local etcd",
			cfg: &kubeadmapi.Etcd{
				Local: &kubeadmapi.LocalEtcd{},
			},
			expected: false,
		},
		{
			name:     "empty etcd struct",
			cfg:      &kubeadmapi.Etcd{},
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if isHA := CheckConfigurationIsHA(test.cfg); isHA != test.expected {
				t.Errorf("expected isHA to be %v, got %v", test.expected, isHA)
			}
		})
	}
}

func testGetURL(t *testing.T, getURLFunc func(*kubeadmapi.InitConfiguration) string, port int) {
	portStr := strconv.Itoa(port)
	var tests = []struct {
		name             string
		advertiseAddress string
		expectedURL      string
	}{
		{
			name:             "IPv4",
			advertiseAddress: "10.10.10.10",
			expectedURL:      fmt.Sprintf("https://10.10.10.10:%s", portStr),
		},
		{
			name:             "IPv6",
			advertiseAddress: "2001:db8::2",
			expectedURL:      fmt.Sprintf("https://[2001:db8::2]:%s", portStr),
		},
		{
			name:             "IPv4 localhost",
			advertiseAddress: "127.0.0.1",
			expectedURL:      fmt.Sprintf("https://127.0.0.1:%s", portStr),
		},
		{
			name:             "IPv6 localhost",
			advertiseAddress: "::1",
			expectedURL:      fmt.Sprintf("https://[::1]:%s", portStr),
		},
	}

	for _, test := range tests {
		cfg := &kubeadmapi.InitConfiguration{
			LocalAPIEndpoint: kubeadmapi.APIEndpoint{
				AdvertiseAddress: test.advertiseAddress,
			},
		}
		url := getURLFunc(cfg)
		if url != test.expectedURL {
			t.Errorf("expected %s, got %s", test.expectedURL, url)
		}
	}
}

func TestGetClientURL(t *testing.T) {
	testGetURL(t, GetClientURL, constants.EtcdListenClientPort)
}

func TestGetPeerURL(t *testing.T) {
	testGetURL(t, GetClientURL, constants.EtcdListenClientPort)
}

func TestGetClientURLByIP(t *testing.T) {
	portStr := strconv.Itoa(constants.EtcdListenClientPort)
	var tests = []struct {
		name        string
		ip          string
		expectedURL string
	}{
		{
			name:        "IPv4",
			ip:          "10.10.10.10",
			expectedURL: fmt.Sprintf("https://10.10.10.10:%s", portStr),
		},
		{
			name:        "IPv6",
			ip:          "2001:db8::2",
			expectedURL: fmt.Sprintf("https://[2001:db8::2]:%s", portStr),
		},
		{
			name:        "IPv4 localhost",
			ip:          "127.0.0.1",
			expectedURL: fmt.Sprintf("https://127.0.0.1:%s", portStr),
		},
		{
			name:        "IPv6 localhost",
			ip:          "::1",
			expectedURL: fmt.Sprintf("https://[::1]:%s", portStr),
		},
	}

	for _, test := range tests {
		url := GetClientURLByIP(test.ip)
		if url != test.expectedURL {
			t.Errorf("expected %s, got %s", test.expectedURL, url)
		}
	}
}
