package blizzard

import (
	"testing"

	"wowapp/internal/config"
)

func TestSlug(t *testing.T) {
	if got := Slug(config.Character{Name: " Leafy", Surname: "Oakheart "}); got != "leafy-oakheart" {
		t.Errorf("Slug = %q, want leafy-oakheart", got)
	}
}

func TestCharPathEscapesName(t *testing.T) {
	got := CharPath(config.Character{Name: "Ælfwine", Surname: "Oak"})
	want := "/profile/wow/character/%C3%A6lfwine-oak"
	if got != want {
		t.Errorf("CharPath = %q, want %q", got, want)
	}
}
