package config

// Diff describes what changed between a previously-applied config and a
// newly-desired one, in terms of which modules need to start or stop.
// Modules that are enabled in both, or disabled in both, are left alone —
// docker compose up -d is idempotent, so re-applying an unchanged module is
// a safe no-op and isn't listed here as "start".
type Diff struct {
	ToStart []string
	ToStop  []string
}

// DiffEnabled compares the previously-applied config against the desired
// one and reports which modules need to be started or stopped to reconcile.
func DiffEnabled(previous, desired *Config) Diff {
	prevSet := map[string]bool{}
	if previous != nil {
		prevSet = previous.EnabledSet()
	}
	desiredSet := desired.EnabledSet()

	var d Diff
	for name, on := range desiredSet {
		if on && !prevSet[name] {
			d.ToStart = append(d.ToStart, name)
		}
	}
	for name, was := range prevSet {
		if was && !desiredSet[name] {
			d.ToStop = append(d.ToStop, name)
		}
	}
	return d
}
