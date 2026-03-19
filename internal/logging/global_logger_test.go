package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	log "github.com/sirupsen/logrus"
)

func TestConfigureLogOutput_WritesErrorEntriesToDedicatedFile(t *testing.T) {
	t.Setenv("WRITABLE_PATH", t.TempDir())
	t.Cleanup(func() {
		if err := ConfigureLogOutput(&config.Config{}); err != nil {
			t.Fatalf("reset log output: %v", err)
		}
	})

	cfg := &config.Config{LoggingToFile: true}
	if err := ConfigureLogOutput(cfg); err != nil {
		t.Fatalf("ConfigureLogOutput: %v", err)
	}

	log.Info("info only entry")
	log.Error("error entry")
	if logWriter != nil {
		_ = logWriter.Close()
	}
	if errorLogWriter != nil {
		_ = errorLogWriter.Close()
	}

	logDir := ResolveLogDirectory(cfg)
	mainData, err := os.ReadFile(filepath.Join(logDir, "main.log"))
	if err != nil {
		t.Fatalf("read main.log: %v", err)
	}
	errorData, err := os.ReadFile(filepath.Join(logDir, "error.log"))
	if err != nil {
		t.Fatalf("read error.log: %v", err)
	}

	mainText := string(mainData)
	errorText := string(errorData)
	if !strings.Contains(mainText, "info only entry") || !strings.Contains(mainText, "error entry") {
		t.Fatalf("main.log missing expected entries: %s", mainText)
	}
	if strings.Contains(errorText, "info only entry") {
		t.Fatalf("error.log should not contain info entries: %s", errorText)
	}
	if !strings.Contains(errorText, "error entry") {
		t.Fatalf("error.log missing error entry: %s", errorText)
	}
}
