package blizzard

import (
	"testing"

	"wowapp/internal/config"
)

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Argent Dawn":       "argent-dawn",
		"Kel'Thuzad":        "kelthuzad",
		"stormrage-forever": "stormrage-forever",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCharPathEscapesName(t *testing.T) {
	got := CharPath(config.Character{Realm: "Argent Dawn", Name: "Ælfwine"})
	want := "/profile/wow/character/argent-dawn/%C3%A6lfwine"
	if got != want {
		t.Errorf("CharPath = %q, want %q", got, want)
	}
}
