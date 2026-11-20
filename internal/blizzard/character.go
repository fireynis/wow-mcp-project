package blizzard

import (
	"net/url"
	"strings"

	"wowapp/internal/config"
)

// Slug is a character's API slug: name and surname lowercased and joined by a
// hyphen ("Leafy" "Oakheart" -> "leafy-oakheart"). WoW Forever has no realms.
func Slug(c config.Character) string {
	return strings.ToLower(strings.TrimSpace(c.Name)) + "-" + strings.ToLower(strings.TrimSpace(c.Surname))
}

// CharPath is the profile path of a character, its slug path-escaped.
func CharPath(c config.Character) string {
	return "/profile/wow/character/" + url.PathEscape(Slug(c))
}
