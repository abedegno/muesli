package main

import (
	"fmt"
	"strings"
)

// readyBanner returns the startup banner printed once the server is fully
// initialized — migrations applied, templates seeded, default plugins
// registered, worker pool running — and about to serve. In other words: when
// everything is up. Plain stdout (no log timestamps) keeps the art clean.
func readyBanner(publicURL string) string {
	base := strings.TrimRight(publicURL, "/")
	return fmt.Sprintf(`
          _ . _ . _ . _ . _
       ~ ( o · * ~ · o · * ) ~      muesli is served  🥣
          \_______________/
           \_____________/

   Everything is up.
     Admin UI :  %s/admin
     API      :  %s
     Health   :  %s/healthz
     Ready    :  %s/readyz

`, base, base, base, base)
}
