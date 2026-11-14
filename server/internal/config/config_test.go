package config

import "testing"

func TestStatsAndASCComeOnlyFromEnv(t *testing.T) {
	for _, key := range []string{
		"ADMIN_PASSWORD", "ADMIN_PASSWORD_HASH", "ADMIN_HOST", "STATS_TEST_EMAILS",
		"ASC_KEY_ID", "ASC_ISSUER_ID", "ASC_KEY_PATH", "ASC_APP_ID",
	} {
		t.Setenv(key, "")
	}
	cfg := FromEnv()
	if cfg.AdminPassword != "" || cfg.AdminPasswordHash != "" || cfg.AdminHost != "" {
		t.Fatal("admin settings were not empty")
	}
	if len(cfg.StatsTestEmails) != 0 || cfg.ASCKeyID != "" || cfg.ASCIssuerID != "" || cfg.ASCKeyPath != "" || cfg.ASCAppID != "" {
		t.Fatal("stats or ASC defaults were not empty")
	}
	t.Setenv("STATS_TEST_EMAILS", " A@Example.com, ,b@example.com ")
	t.Setenv("ADMIN_HOST", "DL.Xiaomoto.Cloud")
	t.Setenv("ASC_KEY_ID", "KEY")
	cfg = FromEnv()
	if cfg.AdminHost != "dl.xiaomoto.cloud" {
		t.Fatalf("host %q", cfg.AdminHost)
	}
	if len(cfg.StatsTestEmails) != 2 || cfg.StatsTestEmails[0] != "a@example.com" || cfg.StatsTestEmails[1] != "b@example.com" {
		t.Fatalf("emails %#v", cfg.StatsTestEmails)
	}
	if cfg.ASCKeyID != "KEY" || cfg.ASCIssuerID != "" {
		t.Fatal("partial ASC was filled in")
	}
}
