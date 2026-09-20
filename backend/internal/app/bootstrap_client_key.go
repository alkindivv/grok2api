package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	clientkeyapp "github.com/chenyme/grok2api/backend/internal/application/clientkey"
	clientkeydomain "github.com/chenyme/grok2api/backend/internal/domain/clientkey"
	"github.com/chenyme/grok2api/backend/internal/infra/config"
)

func ensureBootstrapBuildClientKey(ctx context.Context, service *clientkeyapp.Service, cfg config.BootstrapClientKeyConfig) error {
	name := strings.TrimSpace(cfg.Name)
	keyFile := strings.TrimSpace(cfg.KeyFile)
	if value := strings.TrimSpace(os.Getenv("GROK2API_BOOTSTRAP_CLIENT_KEY_NAME")); value != "" {
		name = value
	}
	if value := strings.TrimSpace(os.Getenv("GROK2API_BOOTSTRAP_CLIENT_KEY_FILE")); value != "" {
		keyFile = value
	}
	if name == "" && keyFile == "" {
		return nil
	}
	if name == "" || keyFile == "" {
		return errors.New("bootstrap client key config is incomplete")
	}
	data, err := os.ReadFile(keyFile)
	if err == nil {
		_, err = service.EnsureBootstrapBuildKey(ctx, name, strings.TrimSpace(string(data)))
		return err
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read bootstrap client key file: %w", err)
	}

	created, err := service.Create(ctx, clientkeyapp.CreateInput{
		Name: name, Enabled: true, RPMUnlimited: true, ConcurrencyUnlimited: true,
		ProviderScope: clientkeydomain.ProviderScopeBuild, TierScope: clientkeydomain.TierScopeAll,
	})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o700); err != nil {
		return fmt.Errorf("create bootstrap client key directory: %w", err)
	}
	file, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create bootstrap client key file: %w", err)
	}
	if _, err := file.WriteString(created.Secret + "\n"); err != nil {
		_ = file.Close()
		return fmt.Errorf("write bootstrap client key file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close bootstrap client key file: %w", err)
	}
	return nil
}
