package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clientkeyapp "github.com/chenyme/grok2api/backend/internal/application/clientkey"
	accountdomain "github.com/chenyme/grok2api/backend/internal/domain/account"
	modeldomain "github.com/chenyme/grok2api/backend/internal/domain/model"
	"github.com/chenyme/grok2api/backend/internal/infra/config"
	"github.com/chenyme/grok2api/backend/internal/infra/persistence/relational"
	"github.com/chenyme/grok2api/backend/internal/infra/provider"
	"github.com/chenyme/grok2api/backend/internal/infra/security"
)

func TestBootstrapClientKeyRollsBackDatabaseWhenKeyFileCannotBeCreated(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	database, err := relational.OpenSQLite(ctx, filepath.Join(root, "bootstrap-client-key.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	cipher, err := security.NewCipher("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if err != nil {
		t.Fatal(err)
	}
	service := clientkeyapp.NewService(relational.NewClientKeyRepository(database), nil, nil, 60, 5, cipher)
	keyFile := filepath.Join(root, "client.key")
	if err := os.Symlink(filepath.Join(root, "missing-target"), keyFile); err != nil {
		t.Fatal(err)
	}

	err = ensureBootstrapBuildClientKey(ctx, service, config.BootstrapClientKeyConfig{
		Name: "hermes-agent-3", KeyFile: keyFile,
	})
	if err == nil {
		t.Fatal("expected bootstrap key-file creation failure")
	}
	if !strings.Contains(err.Error(), "create bootstrap client key file") {
		t.Fatalf("bootstrap failed before key-file creation branch: %v", err)
	}
	values, total, listErr := service.List(ctx, 1, 20, "", clientkeyapp.ListFilter{})
	if listErr != nil {
		t.Fatal(listErr)
	}
	if total != 0 || len(values) != 0 {
		t.Fatalf("orphan bootstrap key remained after file failure: total=%d values=%#v", total, values)
	}
}

func TestReadinessStartupReportDoesNotExposeInternalErrors(t *testing.T) {
	state := newStartupState(0)
	state.recordError(errors.New("postgres://private-host/internal"))
	_, _, report, _ := state.snapshot()
	payload, err := json.Marshal(newReadinessStartupReport(report))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "private-host") || !strings.Contains(string(payload), `"errorCount":1`) {
		t.Fatalf("public readiness leaked internal error: %s", payload)
	}
}

func TestReadinessKeepsBuildReadyWhenWebIsUnavailable(t *testing.T) {
	ctx := context.Background()
	database, err := relational.OpenSQLite(ctx, filepath.Join(t.TempDir(), "readiness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	accounts := relational.NewAccountRepository(database)
	models := relational.NewModelRepository(database)
	now := time.Now().UTC()
	build, _, err := accounts.UpsertByIdentity(ctx, accountdomain.Credential{
		Provider: accountdomain.ProviderBuild, Name: "build-ready", SourceKey: "build-ready",
		EncryptedAccessToken: "access", EncryptedRefreshToken: "refresh", ExpiresAt: now.Add(time.Hour),
		Enabled: true, AuthStatus: accountdomain.AuthStatusActive, MaxConcurrent: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := models.UpsertRoutes(ctx, []modeldomain.Route{
		{PublicID: "build-model", Provider: accountdomain.ProviderBuild, UpstreamModel: "build-model", Capability: modeldomain.CapabilityResponses, Enabled: true},
		{PublicID: "web-model", Provider: accountdomain.ProviderWeb, UpstreamModel: "web-model", Capability: modeldomain.CapabilityChat, Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := models.ReplaceAccountCapabilities(ctx, build.ID, []string{"build-model"}, now); err != nil {
		t.Fatal(err)
	}
	state := newStartupState(0)
	state.setPhase("running")
	state.setStatsig("unavailable", "test", 0)
	snapshot := readinessSnapshot(ctx, state, func(context.Context) error { return nil }, models, accounts, provider.NewRegistry(), nil)
	if !snapshot.Ready || snapshot.State != "degraded" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.Components["grok_build"].State != "ready" || snapshot.Components["grok_web"].State != "unavailable" {
		t.Fatalf("components = %#v", snapshot.Components)
	}
}

func TestReadinessRejectsAccountWithoutAccessToken(t *testing.T) {
	ctx := context.Background()
	database, err := relational.OpenSQLite(ctx, filepath.Join(t.TempDir(), "readiness-missing-access.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	accounts := relational.NewAccountRepository(database)
	models := relational.NewModelRepository(database)
	now := time.Now().UTC()
	build, _, err := accounts.UpsertByIdentity(ctx, accountdomain.Credential{
		Provider: accountdomain.ProviderBuild, AuthType: accountdomain.AuthTypeOAuth,
		Name: "refresh-only", SourceKey: "refresh-only", EncryptedRefreshToken: "refresh",
		ExpiresAt: now.Add(time.Hour), Enabled: true, AuthStatus: accountdomain.AuthStatusActive, MaxConcurrent: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := models.UpsertRoutes(ctx, []modeldomain.Route{{
		PublicID: "build-model", Provider: accountdomain.ProviderBuild, UpstreamModel: "build-model", Capability: modeldomain.CapabilityResponses, Enabled: true,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := models.ReplaceAccountCapabilities(ctx, build.ID, []string{"build-model"}, now); err != nil {
		t.Fatal(err)
	}
	state := newStartupState(0)
	state.setPhase("running")
	snapshot := readinessSnapshot(ctx, state, func(context.Context) error { return nil }, models, accounts, provider.NewRegistry(), nil)
	if snapshot.Ready || snapshot.Components["grok_build"].State != "unavailable" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestReadinessRestoresPersistedCooldownWithoutUpstreamProbe(t *testing.T) {
	ctx := context.Background()
	database, err := relational.OpenSQLite(ctx, filepath.Join(t.TempDir(), "cooldown-readiness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	accounts := relational.NewAccountRepository(database)
	models := relational.NewModelRepository(database)
	now := time.Now().UTC()
	cooldownUntil := now.Add(10 * time.Minute)
	build, _, err := accounts.UpsertByIdentity(ctx, accountdomain.Credential{
		Provider: accountdomain.ProviderBuild, Name: "cooling", SourceKey: "cooling",
		EncryptedAccessToken: "access", EncryptedRefreshToken: "refresh", ExpiresAt: now.Add(time.Hour),
		Enabled: true, AuthStatus: accountdomain.AuthStatusActive, MaxConcurrent: 1, CooldownUntil: &cooldownUntil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := models.UpsertRoutes(ctx, []modeldomain.Route{{PublicID: "build-model", Provider: accountdomain.ProviderBuild, UpstreamModel: "build-model", Capability: modeldomain.CapabilityResponses, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if err := models.ReplaceAccountCapabilities(ctx, build.ID, []string{"build-model"}, now); err != nil {
		t.Fatal(err)
	}
	state := newStartupState(0)
	state.setPhase("running")
	snapshot := readinessSnapshot(ctx, state, func(context.Context) error { return nil }, models, accounts, provider.NewRegistry(), nil)
	if snapshot.Ready || snapshot.State != "not_ready" || snapshot.Components["grok_build"].State != "unavailable" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}
