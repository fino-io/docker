package docker

import (
	"errors"
	"strings"
)

const (
	OptionWorkingDir  = "workingDir"
	OptionEnv         = "env"
	OptionCpuSet      = "cpuset"
	OptionPorts       = "ports"
	OptionMemoryLimit = "memory"
	OptionAddHost     = "add-host"
	OptionNetwork     = "network"
	OptionTimeout     = "timeout"
)

var ErrOOMKilled = errors.New("the container was oom-killed")

func SimplifyId(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
