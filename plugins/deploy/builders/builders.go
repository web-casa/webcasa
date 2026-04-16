// Package builders provides a multi-builder abstraction for project deployment.
// Supported builders: Dockerfile, Nixpacks, Paketo, Railpack, Static.
package builders

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// BuildContext holds the parameters for a build operation.
type BuildContext struct {
	ProjectDir string // path to the project source code
	ImageTag   string // desired Docker image tag (e.g., "webcasa-project-5:3")
	AppName    string // application name
}

// Builder is the interface for all build strategies.
type Builder interface {
	Name() string
	Detect(projectDir string) bool // returns true if this builder can handle the project
	Available() bool               // returns true if the builder CLI is installed
}

// DetectBuilder auto-detects the best builder for a project directory.
// Priority: Dockerfile > Nixpacks > Paketo > Railpack > Static
func DetectBuilder(projectDir string) string {
	// 1. Dockerfile exists → use it
	if fileExists(filepath.Join(projectDir, "Dockerfile")) {
		return "dockerfile"
	}

	// 2. Ruby with Gemfile → prefer railpack if available, else nixpacks
	if fileExists(filepath.Join(projectDir, "Gemfile")) {
		if IsAvailable("railpack") {
			return "railpack"
		}
		return "nixpacks"
	}

	// 3. Other language-specific files → Nixpacks
	langFiles := []string{"package.json", "go.mod", "requirements.txt",
		"pom.xml", "build.gradle", "Cargo.toml", "mix.exs", "composer.json"}
	for _, f := range langFiles {
		if fileExists(filepath.Join(projectDir, f)) {
			return "nixpacks"
		}
	}

	// 4. Only static files
	staticExts := []string{".html", ".htm"}
	for _, ext := range staticExts {
		matches, _ := filepath.Glob(filepath.Join(projectDir, "*"+ext))
		if len(matches) > 0 {
			return "static"
		}
	}

	// Default to nixpacks (it can auto-detect many setups)
	return "nixpacks"
}

// IsAvailable checks if a builder CLI is installed on the system.
func IsAvailable(builderType string) bool {
	switch builderType {
	case "dockerfile":
		_, err := exec.LookPath("docker")
		return err == nil
	case "nixpacks":
		_, err := exec.LookPath("nixpacks")
		return err == nil
	case "paketo":
		_, err := exec.LookPath("pack")
		return err == nil
	case "railpack":
		_, err := exec.LookPath("railpack")
		return err == nil
	case "static":
		_, err := exec.LookPath("docker")
		return err == nil // static uses nginx Docker image
	default:
		return false
	}
}

// InstallCommand returns the shell command to install a builder.
func InstallCommand(builderType string) string {
	switch builderType {
	case "nixpacks":
		return "curl -sSL https://nixpacks.com/install.sh | bash"
	case "paketo":
		return "(curl -sSL 'https://github.com/buildpacks/pack/releases/latest/download/pack-v0.36.4-linux.tgz' | tar -xz -C /usr/local/bin)"
	case "railpack":
		return "curl -sSL https://railpack.com/install.sh | bash"
	default:
		return ""
	}
}

// ValidBuilderTypes is the set of valid build_type values.
var ValidBuilderTypes = map[string]bool{
	"":           true, // legacy/default
	"auto":       true,
	"dockerfile": true,
	"nixpacks":   true,
	"paketo":     true,
	"railpack":   true,
	"static":     true,
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// BuildCommand returns the Docker build command for a given builder type.
func BuildCommand(builderType, projectDir, imageTag string) (string, []string, error) {
	switch builderType {
	case "dockerfile", "":
		return "docker", []string{"build", "-t", imageTag, "."}, nil
	case "nixpacks":
		return "nixpacks", []string{"build", projectDir, "--name", imageTag}, nil
	case "paketo":
		return "pack", []string{"build", imageTag, "--path", projectDir, "--builder", "paketobuildpacks/builder-jammy-full"}, nil
	case "railpack":
		return "railpack", []string{"build", "--path", projectDir, "--tag", imageTag}, nil
	case "static":
		// Generate a simple Dockerfile for static files
		dockerfile := "FROM nginx:alpine\nCOPY . /usr/share/nginx/html/\n"
		if err := os.WriteFile(filepath.Join(projectDir, "Dockerfile.static"), []byte(dockerfile), 0644); err != nil {
			return "", nil, fmt.Errorf("write static Dockerfile: %w", err)
		}
		return "docker", []string{"build", "-t", imageTag, "-f", "Dockerfile.static", "."}, nil
	default:
		return "", nil, fmt.Errorf("unsupported builder type: %s", builderType)
	}
}
