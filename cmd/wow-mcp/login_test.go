package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoginRejectsArguments(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runIO([]string{"login", "extra"}, strings.NewReader(""), &out, &errb); code != 2 {
		t.Fatalf("exit = %d", code)
	}
}

func TestUsageListsLogin(t *testing.T) {
	if !strings.Contains(usage, "login") {
		t.Fatal("usage does not mention login")
	}
}
