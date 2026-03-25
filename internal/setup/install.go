package setup

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const defaultCLIPackage = "@wuphf/wuphf"

// InstallLatestCLI installs the latest published CLI from npm.
// The package and installer binary can be overridden for tests via env vars.
func InstallLatestCLI() (string, error) {
	bin := strings.TrimSpace(os.Getenv("WUPHF_CLI_INSTALL_BIN"))
	if bin == "" {
		bin = "npm"
	}
	pkg := strings.TrimSpace(os.Getenv("WUPHF_CLI_PACKAGE"))
	if pkg == "" {
		pkg = defaultCLIPackage
	}

	path, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("%s is required to install the latest CLI from npm", bin)
	}

	cmd := exec.Command(path, "install", "-g", pkg+"@latest")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if trimmed := strings.TrimSpace(string(output)); trimmed != "" {
			return "", fmt.Errorf("install latest CLI: %s", trimmed)
		}
		return "", fmt.Errorf("install latest CLI: %w", err)
	}

	return fmt.Sprintf("Latest %s CLI installed from npm.", pkg), nil
}
