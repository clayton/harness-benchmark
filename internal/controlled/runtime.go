package controlled

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Runtime is the small OCI CLI contract used by controlled execution.
type Runtime struct {
	Name    string `json:"name"`
	Command string `json:"-"`
	Version string `json:"version"`
	Arch    string `json:"architecture"`
}

func SelectRuntime(request string) (Runtime, error) {
	if request == "" {
		request = "auto"
	}
	candidates := []string{"docker", "podman", "nerdctl"}
	if request != "auto" {
		if request != "docker" && request != "podman" && request != "nerdctl" {
			return Runtime{}, fmt.Errorf("unsupported OCI runtime %q (choose auto, docker, podman, or nerdctl)", request)
		}
		candidates = []string{request}
	}
	for _, name := range candidates {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		version, _ := commandOutput(context.Background(), path, "--version")
		return Runtime{Name: name, Command: path, Version: strings.TrimSpace(string(version)), Arch: runtime.GOARCH}, nil
	}
	if request == "auto" {
		return Runtime{}, fmt.Errorf("no OCI runtime found; install Docker, Podman, or nerdctl, then rerun (Colima and Dory work through Docker-compatible contexts)")
	}
	return Runtime{}, fmt.Errorf("OCI runtime %q is not installed", request)
}

func (r Runtime) Run(ctx context.Context, args ...string) ([]byte, error) {
	return commandOutput(ctx, r.Command, args...)
}

func imageDigest(image string) string {
	if at := strings.LastIndex(image, "@sha256:"); at >= 0 {
		return image[at+1:]
	}
	return ""
}
