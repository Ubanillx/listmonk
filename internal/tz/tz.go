// Package tz resolves the one time zone the whole application uses.
//
// The deployment decides it: the TZ environment variable, the host's
// /etc/localtime (or /etc/timezone), and only as a last resort the
// Asia/Shanghai default. The optional app.timezone setting overrides the
// deployment for an instance that has to differ, and nothing else may keep a
// clock of its own.
//
// Use Now/In from this package instead of time.Now().In(time.Local), and hand
// the resolved *time.Location to anything that formats or truncates dates.
package tz

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Default is used when neither the deployment nor the configuration names a
// usable zone.
const Default = "Asia/Shanghai"

var (
	mu   sync.RWMutex
	loc  = time.Local
	name = ""
)

// Resolve picks the application time zone, in order:
//
//  1. the configured name (app.timezone / LISTMONK_app__timezone);
//  2. TZ, which is what Docker and systemd deployments set;
//  3. the host zone from /etc/timezone or the /etc/localtime symlink;
//  4. the process zone as time.Local sees it (an unnamed zone), else Default.
//
// The returned name is an IANA name the database and the admin UI can use. It
// is empty when the host zone cannot be named; callers fall back to Default.
func Resolve(configured string) (*time.Location, string) {
	for _, candidate := range []string{configured, envZone(), hostZone()} {
		if candidate = strings.TrimSpace(candidate); candidate == "" || isLocalAlias(candidate) {
			continue
		}
		if l, err := time.LoadLocation(candidate); err == nil {
			return l, candidate
		} else if candidate == strings.TrimSpace(configured) && candidate != "" {
			log.Printf("invalid app.timezone %q (%v); falling back to the deployment time zone", candidate, err)
		}
	}

	// The host zone is known to the runtime but has no IANA name.
	if time.Local != nil && time.Local != time.UTC {
		return time.Local, ""
	}
	if l, err := time.LoadLocation(Default); err == nil {
		return l, Default
	}
	return time.Local, ""
}

// isLocalAlias reports names that mean "whatever the process uses", which must
// not be loaded as if they were an IANA zone.
func isLocalAlias(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "local", "localtime", "default":
		return true
	}
	return false
}

// envZone returns the TZ value the process was started with.
func envZone() string {
	return strings.TrimPrefix(strings.TrimSpace(os.Getenv("TZ")), ":")
}

// hostZone names the host zone without relying on TZ. Containers usually set
// TZ, but a bare binary on Linux has only these files.
func hostZone() string {
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if name := strings.TrimSpace(string(b)); name != "" && !strings.ContainsAny(name, " \n\t") {
			if _, err := time.LoadLocation(name); err == nil {
				return name
			}
		}
	}
	if target, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if idx := strings.Index(target, "zoneinfo/"); idx >= 0 {
			if name := strings.TrimPrefix(target[idx+len("zoneinfo/"):], "/"); name != "" {
				if _, err := time.LoadLocation(name); err == nil {
					return name
				}
			}
		}
	}
	return ""
}

// Init resolves and installs the process-wide time zone. It also points
// time.Local at the resolved zone, so the standard library, the cron
// scheduler and any remaining time.Local caller agree with this package.
func Init(configured string) *time.Location {
	l, n := Resolve(configured)
	mu.Lock()
	loc, name = l, n
	mu.Unlock()

	time.Local = l
	if n != "" {
		if tz := envZone(); tz != n {
			_ = os.Setenv("TZ", n)
		}
	}

	log.Printf("application time zone: %s (current time %s)", Name(), Now().Format(time.RFC3339))
	return l
}

// Location returns the resolved time zone.
func Location() *time.Location {
	mu.RLock()
	defer mu.RUnlock()
	return loc
}

// Name returns the resolved time zone's IANA name, or Default when the host
// zone could not be named. This is the value the database session and the
// admin UI use, so it is always a loadable name.
func Name() string {
	mu.RLock()
	defer mu.RUnlock()
	if name == "" {
		return Default
	}
	return name
}

// DBName returns the zone name to pin on database sessions. It is Name(), and
// therefore always a name PostgreSQL accepts.
func DBName() string {
	return Name()
}

// Now returns the current time in the application time zone.
func Now() time.Time {
	return time.Now().In(Location())
}

// In converts t to the application time zone.
func In(t time.Time) time.Time {
	return t.In(Location())
}

// OffsetSeconds returns the zone's current offset from UTC in seconds,
// including daylight saving when the zone observes it.
func OffsetSeconds() int {
	_, offset := Now().Zone()
	return offset
}

// OffsetLabel renders the current offset as +08:00 style text.
func OffsetLabel() string {
	return FormatOffset(OffsetSeconds())
}

// FormatOffset renders a UTC offset in seconds as +HH:MM.
func FormatOffset(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
	}
	return fmt.Sprintf("%s%02d:%02d", sign, seconds/3600, (seconds%3600)/60)
}
