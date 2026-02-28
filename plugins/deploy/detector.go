package deploy

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// DetectFramework analyses a project directory and returns the best-matching framework preset.
func DetectFramework(dir string) FrameworkPreset {
	// Check package.json for Node.js frameworks
	if preset := detectNodeFramework(dir); preset != nil {
		return *preset
	}

	// Check go.mod for Go projects
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return frameworkPresets["go"]
	}

	// Check composer.json for PHP/Laravel
	if preset := detectPHPFramework(dir); preset != nil {
		return *preset
	}

	// Check requirements.txt / manage.py for Python
	if preset := detectPythonFramework(dir); preset != nil {
		return *preset
	}

	return frameworkPresets["custom"]
}

// DetectFrameworkFromURL clones a repo temporarily and detects the framework.
// This is used by the "detect" API endpoint before a project is created.
func DetectFrameworkFromURL(url, branch string) (FrameworkPreset, error) {
	tmpDir, err := os.MkdirTemp("", "detect_*")
	if err != nil {
		return frameworkPresets["custom"], err
	}
	defer os.RemoveAll(tmpDir)

	gc := NewGitClient(tmpDir)
	lw := &LogWriter{} // discard output
	// Use a fake project ID; the dir will be tmpDir/project_0
	if err := gc.Clone(url, branch, "", 0, lw); err != nil {
		return frameworkPresets["custom"], err
	}

	return DetectFramework(gc.ProjectDir(0)), nil
}

func detectNodeFramework(dir string) *FrameworkPreset {
	pkgPath := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return nil
	}

	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
		Scripts         map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}

	allDeps := make(map[string]string)
	for k, v := range pkg.Dependencies {
		allDeps[k] = v
	}
	for k, v := range pkg.DevDependencies {
		allDeps[k] = v
	}

	// Check in priority order
	if _, ok := allDeps["next"]; ok {
		p := frameworkPresets["nextjs"]
		return &p
	}
	if _, ok := allDeps["nuxt"]; ok {
		p := frameworkPresets["nuxt"]
		return &p
	}
	if _, ok := allDeps["@remix-run/node"]; ok {
		p := frameworkPresets["remix"]
		return &p
	}
	if _, ok := allDeps["express"]; ok {
		p := frameworkPresets["express"]
		return &p
	}
	// Fallback: if vite is present, it's likely a SPA
	if _, ok := allDeps["vite"]; ok {
		p := frameworkPresets["vite"]
		return &p
	}

	// Generic Node.js project
	p := FrameworkPreset{
		Name: "Node.js", Framework: "nodejs",
		InstallCmd: "npm install", StartCmd: "node index.js", Port: 3000,
	}
	if _, ok := pkg.Scripts["build"]; ok {
		p.BuildCmd = "npm run build"
	}
	if _, ok := pkg.Scripts["start"]; ok {
		p.StartCmd = "npm start"
	}
	return &p
}

func detectPHPFramework(dir string) *FrameworkPreset {
	composerPath := filepath.Join(dir, "composer.json")
	data, err := os.ReadFile(composerPath)
	if err != nil {
		return nil
	}

	var composer struct {
		Require map[string]string `json:"require"`
	}
	if err := json.Unmarshal(data, &composer); err != nil {
		p := frameworkPresets["laravel"] // fallback to Laravel for any composer project
		return &p
	}

	if _, ok := composer.Require["laravel/framework"]; ok {
		p := frameworkPresets["laravel"]
		return &p
	}

	// Generic PHP
	p := FrameworkPreset{
		Name: "PHP", Framework: "php",
		InstallCmd: "composer install", StartCmd: "php-fpm", Port: 9000,
	}
	return &p
}

func detectPythonFramework(dir string) *FrameworkPreset {
	// Check for Django
	if _, err := os.Stat(filepath.Join(dir, "manage.py")); err == nil {
		p := frameworkPresets["django"]
		return &p
	}

	// Check requirements.txt for flask
	reqPath := filepath.Join(dir, "requirements.txt")
	data, err := os.ReadFile(reqPath)
	if err != nil {
		return nil
	}

	content := string(data)
	if contains(content, "flask") || contains(content, "Flask") {
		p := frameworkPresets["flask"]
		return &p
	}
	if contains(content, "django") || contains(content, "Django") {
		p := frameworkPresets["django"]
		return &p
	}

	// Generic Python
	p := FrameworkPreset{
		Name: "Python", Framework: "python",
		InstallCmd: "pip install -r requirements.txt", StartCmd: "python app.py", Port: 8000,
	}
	return &p
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (len(s) >= len(substr)) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
