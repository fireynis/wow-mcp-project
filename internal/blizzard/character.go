package blizzard

import (
	"net/url"
	"strings"

	"wowapp/internal/config"
)

// Slug turns a realm name into its API slug: lowercase, apostrophes dropped,
// spaces become hyphens ("Kel'Thuzad" -> "kelthuzad").
func Slug(realm string) string {
	s := strings.ToLower(strings.TrimSpace(realm))
	s = strings.NewReplacer("'", "", "’", "").Replace(s)
	return strings.Join(strings.Fields(s), "-")
}

// CharPath is the profile path of a character: the realm slugged, the name
// lowercased and path-escaped.
func CharPath(c config.Character) string {
	return "/profile/wow/character/" + url.PathEscape(Slug(c.Realm)) + "/" + url.PathEscape(strings.ToLower(c.Name))
}
