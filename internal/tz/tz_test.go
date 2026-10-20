package tz

import (
	"testing"
	"time"
)

// TestResolveFollowsDeploymentZone pins the precedence the rest of the app
// relies on: an explicit setting wins, then the deployment's TZ, then the host
// zone, then Asia/Shanghai.
func TestResolveFollowsDeploymentZone(t *testing.T) {
	original := time.Local
	t.Cleanup(func() { time.Local = original })

	t.Run("explicit configuration wins over TZ", func(t *testing.T) {
		t.Setenv("TZ", "Europe/Berlin")
		time.Local, _ = time.LoadLocation("Europe/Berlin")
		loc, name := Resolve("UTC")
		if name != "UTC" || loc.String() != "UTC" {
			t.Fatalf("Resolve(UTC) = %q, %v; want UTC", name, loc)
		}
	})

	t.Run("TZ decides when unconfigured", func(t *testing.T) {
		t.Setenv("TZ", "Asia/Shanghai")
		time.Local, _ = time.LoadLocation("Asia/Shanghai")
		loc, name := Resolve("")
		if name != "Asia/Shanghai" || loc.String() != "Asia/Shanghai" {
			t.Fatalf("Resolve(empty) = %q, %v; want Asia/Shanghai", name, loc)
		}
	})

	t.Run("TZ with a leading colon is still a name", func(t *testing.T) {
		t.Setenv("TZ", ":Europe/Berlin")
		time.Local, _ = time.LoadLocation("Europe/Berlin")
		if _, name := Resolve(""); name != "Europe/Berlin" {
			t.Fatalf("Resolve(empty) = %q; want Europe/Berlin", name)
		}
	})

	t.Run("invalid configuration falls back to TZ", func(t *testing.T) {
		t.Setenv("TZ", "Asia/Shanghai")
		time.Local, _ = time.LoadLocation("Asia/Shanghai")
		_, name := Resolve("Mars/Olympus")
		if name != "Asia/Shanghai" {
			t.Fatalf("Resolve(invalid) = %q; want the deployment zone", name)
		}
	})

	t.Run("abbreviated TZ values are not IANA names", func(t *testing.T) {
		// "CST" is a POSIX abbreviation, not an IANA name. It must not be
		// passed to the database or the UI as a zone name.
		t.Setenv("TZ", "CST-8")
		time.Local = time.FixedZone("CST", 8*3600)
		loc, name := resolveWithHostZone("", "")
		if name != "" || loc != time.Local {
			t.Fatalf("resolveWithHostZone(empty, empty) = %q, %v; want an unnamed host zone", name, loc)
		}

		loc, name = resolveWithHostZone("", "Europe/Berlin")
		if name != "Europe/Berlin" || loc.String() != "Europe/Berlin" {
			t.Fatalf("resolveWithHostZone(empty, Europe/Berlin) = %q, %v; want the named host zone", name, loc)
		}
	})
}

// TestInitInstallsProcessZone guards the reason this package exists: after
// Init, the standard library's time.Local agrees with the configured zone, so
// cron schedules and any remaining time.Local caller cannot drift.
func TestInitInstallsProcessZone(t *testing.T) {
	original := time.Local
	t.Cleanup(func() {
		time.Local = original
		Init(original.String())
	})

	Init("Asia/Shanghai")
	if got := Name(); got != "Asia/Shanghai" {
		t.Fatalf("Name() = %q, want Asia/Shanghai", got)
	}
	if time.Local.String() != "Asia/Shanghai" {
		t.Fatalf("time.Local = %q, want Asia/Shanghai", time.Local)
	}
	if _, offset := Now().Zone(); offset != 8*3600 {
		t.Fatalf("Now() offset = %d, want 28800", offset)
	}
	if got := OffsetLabel(); got != "+08:00" {
		t.Fatalf("OffsetLabel() = %q, want +08:00", got)
	}
}

// TestNameFallsBackToADatabaseSafeName keeps a nameless host zone (for example
// a POSIX-only TZ in a minimal container) from sending an empty zone to
// PostgreSQL or the admin UI.
func TestNameFallsBackToADatabaseSafeName(t *testing.T) {
	mu.Lock()
	original := name
	name = ""
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		name = original
		mu.Unlock()
	})
	if got := Name(); got != Default {
		t.Fatalf("Name() = %q, want %q", got, Default)
	}
	if _, err := time.LoadLocation(DBName()); err != nil {
		t.Fatalf("DBName() = %q is not loadable: %v", DBName(), err)
	}
}

func TestFormatOffset(t *testing.T) {
	for _, tc := range []struct {
		seconds int
		want    string
	}{
		{8 * 3600, "+08:00"},
		{5*3600 + 30*60, "+05:30"},
		{-(3*3600 + 30*60), "-03:30"},
		{0, "+00:00"},
	} {
		if got := FormatOffset(tc.seconds); got != tc.want {
			t.Errorf("FormatOffset(%d) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
}
