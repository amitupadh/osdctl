package env

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrintKubeConfigExport(t *testing.T) {
	tests := []struct {
		name     string
		envPath  string
		expected string
	}{
		{
			name:     "Basic path",
			envPath:  "/home/user/ocenv/test",
			expected: "export KUBECONFIG=/home/user/ocenv/test/kubeconfig.json\n",
		},
		{
			name:     "Empty path",
			envPath:  "",
			expected: "export KUBECONFIG=/kubeconfig.json\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := &OcEnv{
				Path: tt.envPath,
			}

			// Capture stdout
			var buf bytes.Buffer
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			env.PrintKubeConfigExport()

			// Restore stdout
			w.Close()
			os.Stdout = oldStdout
			io.Copy(&buf, r)

			assert.Equal(t, tt.expected, buf.String())
		})
	}
}

func TestBinPath(t *testing.T) {
	tests := []struct {
		name     string
		envPath  string
		expected string
	}{
		{
			name:     "Basic path",
			envPath:  "/home/user/ocenv/test",
			expected: "/home/user/ocenv/test/bin",
		},
		{
			name:     "Empty path",
			envPath:  "",
			expected: "/bin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := &OcEnv{
				Path: tt.envPath,
			}
			assert.Equal(t, tt.expected, env.binPath())
		})
	}
}

func TestEnsureFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name     string
		filename string
		setup    func(string) error
		wantFile bool
	}{
		{
			name:     "File does not exist",
			filename: "testfile.txt",
			setup:    nil,
			wantFile: true,
		},
		{
			name:     "File already exists",
			filename: "existing.txt",
			setup: func(path string) error {
				return os.WriteFile(path, []byte("test"), 0644)
			},
			wantFile: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fullPath := tmpDir + "/" + tt.filename

			// Setup
			if tt.setup != nil {
				if err := tt.setup(fullPath); err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
			}

			// Test
			env := &OcEnv{
				Path:    tmpDir,
				Options: &Options{},
			}
			file := env.ensureFile(fullPath)
			if tt.wantFile {
				assert.NotNil(t, file)
				file.Close()
			} else {
				assert.Nil(t, file)
			}

			// Verify file exists
			_, err := os.Stat(fullPath)
			assert.NoError(t, err)
		})
	}
}

func TestDelete(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name    string
		envPath string
		setup   func(string) error
	}{
		{
			name:    "Delete existing directory",
			envPath: "testenv",
			setup: func(path string) error {
				return os.MkdirAll(path, 0755)
			},
		},
		{
			name:    "Delete non-existent directory",
			envPath: "nonexistent",
			setup:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fullPath := tmpDir + "/" + tt.envPath

			// Setup
			if tt.setup != nil {
				if err := tt.setup(fullPath); err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
			}

			// Test
			env := &OcEnv{
				Path: fullPath,
				Options: &Options{
					Alias: tt.envPath,
				},
			}
			env.Delete()

			// Verify
			_, err := os.Stat(fullPath)
			assert.True(t, os.IsNotExist(err), "Directory should not exist after deletion")
		})
	}
}

func TestGenerateLoginCommand(t *testing.T) {
	tests := []struct {
		name     string
		options  *Options
		expected string
	}{
		{
			name: "Cluster login with token",
			options: &Options{
				ClusterId: "test-cluster",
			},
			expected: "ocm cluster login --token test-cluster",
		},
		{
			name: "Individual cluster login",
			options: &Options{
				Username: "testuser",
				Url:      "https://api.test.com:6443",
			},
			expected: "oc login -u testuser https://api.test.com:6443",
		},
		{
			name: "Individual cluster login with password",
			options: &Options{
				Username: "testuser",
				Password: "testpass",
				Url:      "https://api.test.com:6443",
			},
			expected: "oc login -u testuser -p testpass https://api.test.com:6443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := &OcEnv{
				Options: tt.options,
			}
			result := env.generateLoginCommand()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateLoginCommandIndividualClusterPanic(t *testing.T) {
	env := &OcEnv{
		Options: &Options{
			Username: "testuser",
			// No URL set - should panic
		},
	}

	assert.Panics(t, func() {
		env.generateLoginCommandIndividualCluster()
	}, "Should panic when URL is not set")
}

func TestSetup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name    string
		options *Options
		setup   func(string) error
	}{
		{
			name: "New environment setup",
			options: &Options{
				Alias: "test-env",
			},
		},
		{
			name: "Reset existing environment",
			options: &Options{
				Alias:     "test-env",
				ResetEnv:  true,
				ClusterId: "test-cluster",
			},
			setup: func(path string) error {
				return os.MkdirAll(path, 0755)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fullPath := filepath.Join(tmpDir, tt.options.Alias)

			if tt.setup != nil {
				if err := tt.setup(fullPath); err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
			}

			env := &OcEnv{
				Path:    fullPath,
				Options: tt.options,
			}

			env.Setup()

			// Verify environment directory exists
			_, err := os.Stat(fullPath)
			assert.NoError(t, err)

			// Verify bin directory exists
			_, err = os.Stat(filepath.Join(fullPath, "bin"))
			assert.NoError(t, err)

			// Verify environment files exist
			_, err = os.Stat(filepath.Join(fullPath, ".ocenv"))
			assert.NoError(t, err)
			_, err = os.Stat(filepath.Join(fullPath, ".zshenv"))
			assert.NoError(t, err)
		})
	}
}

func TestEnsureEnvDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name     string
		path     string
		setup    func(string) error
		wantDirs []string
	}{
		{
			name: "Create new directory",
			path: "new-env",
			wantDirs: []string{
				"new-env",
			},
		},
		{
			name: "Directory already exists",
			path: "existing-env",
			setup: func(path string) error {
				return os.MkdirAll(path, 0755)
			},
			wantDirs: []string{
				"existing-env",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fullPath := filepath.Join(tmpDir, tt.path)

			if tt.setup != nil {
				if err := tt.setup(fullPath); err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
			}

			env := &OcEnv{
				Path: fullPath,
			}

			env.ensureEnvDir()

			for _, dir := range tt.wantDirs {
				path := filepath.Join(tmpDir, dir)
				_, err := os.Stat(path)
				assert.NoError(t, err, "Directory should exist: %s", dir)
			}
		})
	}
}

func TestCreateBins(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name          string
		options       *Options
		expectedFiles []string
	}{
		{
			name: "Create bins with cluster ID",
			options: &Options{
				ClusterId: "test-cluster",
			},
			expectedFiles: []string{
				"ocl",
				"ocb",
				"ocd",
				"kube_ps1",
				"kube-ps1.sh",
			},
		},
		{
			name: "Create bins with kubeconfig",
			options: &Options{
				Kubeconfig: "test-kubeconfig",
			},
			expectedFiles: []string{
				"ocb",
				"ocd",
				"kube_ps1",
				"kube-ps1.sh",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testPath := filepath.Join(tmpDir, tt.name)
			err := os.MkdirAll(testPath, 0755)
			assert.NoError(t, err)

			env := &OcEnv{
				Path:    testPath,
				Options: tt.options,
			}

			// Create bin directory
			binPath := filepath.Join(testPath, "bin")
			err = os.MkdirAll(binPath, 0755)
			assert.NoError(t, err)

			env.createBins()

			// Verify expected files exist
			for _, file := range tt.expectedFiles {
				path := filepath.Join(binPath, file)
				_, err := os.Stat(path)
				assert.NoError(t, err, "File should exist: %s", file)

				// Verify file permissions
				info, err := os.Stat(path)
				assert.NoError(t, err)
				assert.Equal(t, os.FileMode(0700), info.Mode()&0777)
			}
		})
	}
}

func TestCreateKubeconfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a temporary kubeconfig file
	kubeconfigContent := []byte("test-kubeconfig-content")
	kubeconfigPath := filepath.Join(tmpDir, "test-kubeconfig")
	err = os.WriteFile(kubeconfigPath, kubeconfigContent, 0600)
	assert.NoError(t, err)

	tests := []struct {
		name           string
		options        *Options
		expectedExists bool
		setup          func(string) error
	}{
		{
			name: "Create kubeconfig from file",
			options: &Options{
				Kubeconfig: kubeconfigPath,
			},
			expectedExists: true,
			setup: func(path string) error {
				return os.MkdirAll(path, 0755)
			},
		},
		{
			name:           "No kubeconfig specified",
			options:        &Options{},
			expectedExists: false,
			setup: func(path string) error {
				return os.MkdirAll(path, 0755)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testPath := filepath.Join(tmpDir, tt.name)
			if tt.setup != nil {
				err := tt.setup(testPath)
				assert.NoError(t, err)
			}

			env := &OcEnv{
				Path:    testPath,
				Options: tt.options,
			}

			env.createKubeconfig()

			kubeconfigPath := filepath.Join(testPath, "kubeconfig.json")
			_, err := os.Stat(kubeconfigPath)
			if tt.expectedExists {
				assert.NoError(t, err)

				// Verify content
				content, err := os.ReadFile(kubeconfigPath)
				assert.NoError(t, err)
				assert.Equal(t, kubeconfigContent, content)

				// Verify permissions
				info, err := os.Stat(kubeconfigPath)
				assert.NoError(t, err)
				assert.Equal(t, os.FileMode(0600), info.Mode()&0777)
			} else {
				assert.True(t, os.IsNotExist(err), "File should not exist: %s", kubeconfigPath)
			}
		})
	}
}

func TestKillChildren(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name    string
		content string
	}{
		{
			name: "No .killpids file",
		},
		{
			name:    "Empty .killpids file",
			content: "",
		},
		{
			name:    "Valid PID in file",
			content: "1\n", // Using PID 1 which is init process and won't be killed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testPath := filepath.Join(tmpDir, tt.name)
			err := os.MkdirAll(testPath, 0755)
			assert.NoError(t, err)

			if tt.content != "" {
				err := os.WriteFile(filepath.Join(testPath, ".killpds"), []byte(tt.content), 0644)
				assert.NoError(t, err)
			}

			env := &OcEnv{
				Path: testPath,
			}

			// Capture stdout to prevent test output pollution
			oldStdout := os.Stdout
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stdout = w
			os.Stderr = w

			env.killChildren()

			// Restore stdout
			w.Close()
			os.Stdout = oldStdout
			os.Stderr = oldStderr
			io.Copy(io.Discard, r)

			// Verify .killpids file is removed if it existed
			_, err = os.Stat(filepath.Join(testPath, ".killpds"))
			assert.True(t, os.IsNotExist(err))
		})
	}
}

func TestEnsureEnvVariables(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name         string
		options      *Options
		expectedVars []string
	}{
		{
			name:    "Basic environment variables",
			options: &Options{},
			expectedVars: []string{
				"KUBECONFIG=",
				"OCM_CONFIG=",
				"PS1=",
				"PATH=",
			},
		},
		{
			name: "Environment variables with cluster ID",
			options: &Options{
				ClusterId: "test-cluster",
			},
			expectedVars: []string{
				"KUBECONFIG=",
				"OCM_CONFIG=",
				"PS1=",
				"PATH=",
				"CLUSTERID=test-cluster",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testPath := filepath.Join(tmpDir, tt.name)
			err := os.MkdirAll(testPath, 0755)
			assert.NoError(t, err)

			env := &OcEnv{
				Path:    testPath,
				Options: tt.options,
			}

			env.ensureEnvVariables()

			// Read .ocenv file
			content, err := os.ReadFile(filepath.Join(testPath, ".ocenv"))
			assert.NoError(t, err)

			// Verify each expected variable is present
			for _, expectedVar := range tt.expectedVars {
				assert.True(t, strings.Contains(string(content), expectedVar), "Expected variable %s not found", expectedVar)
			}

			// Verify .zshenv exists and contains source command
			zshenvContent, err := os.ReadFile(filepath.Join(testPath, ".zshenv"))
			assert.NoError(t, err)
			assert.Contains(t, string(zshenvContent), "source .ocenv")
		})
	}
}

func TestStart(t *testing.T) {
	// Step 1: Create temporary directory for OcEnv.Path
	tmpDir := t.TempDir()

	// Step 2: Create a mock .ocenv file with fake env vars
	ocenvPath := filepath.Join(tmpDir, ".ocenv")
	envContent := "FOO=bar\nBAR=baz\n"
	if err := os.WriteFile(ocenvPath, []byte(envContent), 0644); err != nil {
		t.Fatalf("failed to write .ocenv: %v", err)
	}

	// Step 3: Create a mock shell script that just exits (acts as dummy shell)
	shellScript := filepath.Join(tmpDir, "fake-shell.sh")
	scriptContent := "#!/bin/sh\necho 'Mock shell running'\nexit 0\n"
	if err := os.WriteFile(shellScript, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write mock shell script: %v", err)
	}

	// Step 4: Set SHELL env var to mock shell
	t.Setenv("SHELL", shellScript)

	// Step 5: Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Step 6: Set up OcEnv and run Start()
	env := &OcEnv{
		Options: &Options{Alias: "test"},
		Path:    tmpDir,
	}
	// optionally stub killChildren with a real no-op
	env.killChildren() // Call the existing killChildren method

	go func() {
		env.Start()
		w.Close()
	}()

	// Step 7: Read and verify output
	var buf bytes.Buffer
	io.Copy(&buf, r)
	os.Stdout = oldStdout

	output := buf.String()
	if !strings.Contains(output, "Switching to OpenShift environment test") {
		t.Errorf("expected switch message, got: %s", output)
	}
	if !strings.Contains(output, "Exited OpenShift environment") {
		t.Errorf("expected exit message, got: %s", output)
	}
}
