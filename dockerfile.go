package docker

import (
	"os"
	"regexp"
)

var (
	cmdRegex        = regexp.MustCompile(`(?m:^(CMD|cmd).+$)`)
	entryPointRegex = regexp.MustCompile(`(?m:^(ENTRYPOINT|entrypoint).+$)`)
)

func HasRunCommand(file string) bool {
	content, err := os.ReadFile(file)
	if err != nil {
		return false
	}

	return cmdRegex.Match(content) || entryPointRegex.Match(content)
}
