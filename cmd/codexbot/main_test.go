package main

import (
	"strings"
	"testing"
)

func TestStartupBannerUsesRCODBranding(t *testing.T) {
	banner := startupBanner("", "", "")

	if !strings.Contains(banner, "RCOD") {
		t.Fatalf("startup banner should include RCOD branding, got %q", banner)
	}

	if !strings.Contains(banner, "Remote Control On Demand") {
		t.Fatalf("startup banner should include full product name, got %q", banner)
	}

	if !strings.Contains(banner, "by zevro.ai") {
		t.Fatalf("startup banner should include zevro.ai signature, got %q", banner)
	}

	if strings.Contains(banner, "Codex Telegram Bot") {
		t.Fatalf("startup banner should not include outdated branding, got %q", banner)
	}
}
